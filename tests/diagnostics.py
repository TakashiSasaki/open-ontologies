#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Read-only diagnostic probes and privacy assertions against captured journals."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shlex
from urllib.parse import urlsplit
from urllib.request import Request, urlopen
from urllib.error import HTTPError

MARKER = 'OO_DIAGNOSTIC_PRIVATE_SENTINEL_6c9f'
TTL = '<https://example.org/diagnostic> <urn:p> "value" .'


def probe(args):
    spec = importlib.util.spec_from_file_location('client', args.client_script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    c = module.Client(args.url)
    c.rpc('server/discover')
    expected = []
    def call(name, arguments):
        r = c.rpc('tools/call', {'name': name, 'arguments': arguments})
        p = json.loads(r['content'][0]['text'])
        assert r.get('structuredContent') == p
        return r, p
    def snapshot():
        query = {'query': 'SELECT ?s ?p ?o ?g WHERE { { ?s ?p ?o } UNION { GRAPH ?g { ?s ?p ?o } } }'}
        if args.snapshot_api:
            u = urlsplit(args.snapshot_api)
            assert u.scheme == 'http' and u.hostname in ('127.0.0.1', '::1') and u.path == '/api/query'
            assert not u.username and not u.query and not u.fragment
            request = Request(args.snapshot_api, data=json.dumps(query).encode(), headers={'Content-Type': 'application/json'})
            with urlopen(request, timeout=60) as response:
                raw = response.read(128 * 1024 * 1024 + 1)
            assert len(raw) <= 128 * 1024 * 1024, 'admin snapshot exceeds bounded test limit'
            p = json.loads(raw)
        else:
            _, p = call('onto_query', query)
        lines = sorted(json.dumps(row, sort_keys=True) for row in p['results'])
        return {'rows': len(lines), 'sha256': hashlib.sha256('\n'.join(lines).encode()).hexdigest()}
    try:
        before = snapshot()
        cases = [
            ('arguments', {'inline_content': MARKER, 'from': 'turtle', 'path': '/private/' + MARKER, 'to': 'rdfxml'}, 'invalid_arguments', 'conversion.arguments'),
            ('size', {'inline_content': 'x' * (1048576 + 1), 'from': 'turtle', 'to': 'rdfxml'}, 'input_too_large', 'conversion.arguments'),
            ('parse', {'inline_content': 'not RDF ' + MARKER, 'from': 'turtle', 'to': 'rdfxml'}, 'native_tool_error', 'conversion.parse_input'),
            ('render', {'inline_content': TTL, 'from': 'turtle', 'to': 'unsupported-' + MARKER}, 'native_tool_error', 'conversion.render_output'),
            ('iri', {'inline_content': '<file:///private/' + MARKER + '> <urn:p> "value" .', 'from': 'turtle', 'to': 'rdfxml'}, 'inline_file_iri', 'conversion.check_iris'),
        ]
        for name, arguments, code, stage in cases:
            r, p = call('onto_convert', arguments)
            assert r.get('isError') is True and isinstance(p.get('error'), str), name
            expected.append({'case': name, 'error_code': code, 'error_stage': stage,
                             'error_sha256': hashlib.sha256(p['error'].encode()).hexdigest()})
        r, p = call('onto_convert', {'inline_content': TTL, 'from': 'turtle', 'to': 'rdfxml'})
        assert not r.get('isError') and '<rdf:RDF' in p['content']
        shapes = '@prefix sh: <http://www.w3.org/ns/shacl#> . <urn:shape> a sh:NodeShape ; sh:targetNode <urn:s> ; sh:property [sh:path <urn:missing>; sh:minCount 1] .'
        r, p = call('onto_shacl', {'ontology_turtle': '<urn:s> <urn:p> "value" .', 'shapes': shapes, 'inline': True})
        assert not r.get('isError') and p['conforms'] is False
        assert snapshot() == before
        tools = sorted(c.tools(), key=lambda tool: tool['name'])
        result = {'expected': expected, 'shared_quads': before['rows'], 'shared_graph_sha256': before['sha256'], 'shared_graph_unchanged': True,
                  'tool_count': len(tools), 'read_only': sum(bool(t.get('annotations', {}).get('readOnlyHint')) for t in tools),
                  'catalog_sha256': hashlib.sha256(json.dumps(tools, sort_keys=True, separators=(',', ':')).encode()).hexdigest(),
                  'valid_conversion': True, 'negative_finding_is_not_error': True}
        args.output.write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result, indent=2))
    finally:
        c.close()


def check(args):
    raw = args.journal.read_text()
    assert MARKER not in raw, 'raw input/error marker leaked into log'
    records = []
    for line in raw.splitlines():
        row = json.loads(line)
        message = row.get('MESSAGE', '')
        if 'adapted MCP call' not in message and 'MCP upstream response failed' not in message:
            continue
        values = dict(token.split('=', 1) for token in shlex.split(message) if '=' in token)
        records.append(values)
    expected = json.loads(args.expected.read_text())
    refs = [r['error_ref'] for r in records if r.get('is_error') == 'true']
    assert len(refs) == len(set(refs)) and all(refs), 'missing/duplicate failure references'
    results = []
    for case in expected['expected']:
        found = [r for r in records if r.get('error_sha256') == case['error_sha256']
                 and r.get('error_code') == case['error_code'] and r.get('error_stage') == case['error_stage']]
        assert found, ('diagnostic case missing', case['case'])
        assert all(r.get('is_error') == 'true' and r.get('adapter_error') == 'true' for r in found)
        results.append(case['case'])
    assert any(r.get('tool') == 'onto_shacl' and r.get('is_error') == 'false' for r in records)
    assert any(r.get('tool') == 'onto_convert' and r.get('is_error') == 'false' for r in records)
    assert any(r.get('target_format') == 'unrecognized' for r in records)
    backend_limit = any(r.get('error_code') == 'backend_output_limit' and r.get('error_stage') == 'backend.read_response' for r in records)
    if args.require_backend_limit:
        assert backend_limit, 'large-response failure was not classified'
    result = {'matched_cases': results, 'failure_references_unique': True,
              'raw_marker_logged': False, 'negative_finding_logged_as_error': False,
              'shared_graph_unchanged': expected['shared_graph_unchanged'], 'backend_limit_logged': backend_limit}
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))


def large_probe(args):
    # This fixture deliberately mutates ONLY a disposable QA backend. It is not
    # part of the read-only production probes and refuses this host's live ports.
    for value in [args.url, args.native_url]:
        u = urlsplit(value)
        assert u.scheme == 'http' and u.hostname == '127.0.0.1' and u.port not in (18080, 18081, 18084)
    spec = importlib.util.spec_from_file_location('client', args.client_script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    clients = [module.Client(value, modern=False) for value in [args.native_url, args.url]]
    try:
        for c in clients:
            c.rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'large-qa-fixture', 'version': '1'}})
            c.rpc('notifications/initialized', notification=True)
        file = args.data_dir / 'diagnostic-large.ttl'
        with file.open('x') as stream:
            stream.write('<urn:diagnostic:large> <urn:p> "' + 'x' * (11 * 1024 * 1024) + '" .\n')
        r = clients[0].rpc('tools/call', {'name': 'onto_load', 'arguments': {'path': '/data/diagnostic-large.ttl'}})
        assert json.loads(r['content'][0]['text']).get('ok') is True
        try:
            clients[1].rpc('tools/call', {'name': 'onto_query', 'arguments': {'query': 'SELECT ?s ?p ?o WHERE {?s ?p ?o}'}})
        except HTTPError as error:
            assert error.code == 502 and error.read() == b'upstream connection failed\n'
        else:
            raise AssertionError('oversized MCP result unexpectedly bypassed its cap')
        result = {'fixture_literal_bytes': 11 * 1024 * 1024, 'http_status': 502,
                  'original_http_body_preserved': True, 'qa_writes_only': True}
        args.output.write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result, indent=2))
    finally:
        for c in clients:
            c.close()


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest='mode', required=True)
    q = sub.add_parser('probe')
    q.add_argument('url')
    q.add_argument('--client-script', type=Path, default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'))
    q.add_argument('--snapshot-api', help='Optional loopback /api/query for bounded full comparison beyond the MCP response cap')
    q.add_argument('--output', type=Path, required=True)
    q.set_defaults(run=probe)
    q = sub.add_parser('check')
    q.add_argument('--journal', type=Path, required=True)
    q.add_argument('--expected', type=Path, required=True)
    q.add_argument('--output', type=Path, required=True)
    q.set_defaults(run=check)
    q.add_argument('--require-backend-limit', action='store_true')
    q = sub.add_parser('large-probe')
    q.add_argument('url', help='Disposable adapter legacy URL, not the modern proxy')
    q.add_argument('--native-url', required=True)
    q.add_argument('--allow-qa-writes', action='store_true', required=True)
    q.add_argument('--data-dir', type=Path, required=True)
    q.add_argument('--client-script', type=Path, default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'))
    q.add_argument('--output', type=Path, required=True)
    q.set_defaults(run=large_probe)
    args = p.parse_args()
    args.run(args)
