#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Inline conversion/read-only regressions. Policy writes require disposable QA."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
from urllib.parse import urlsplit

TTL = '@prefix ex: <https://example.org/> . @prefix owl: <http://www.w3.org/2002/07/owl#> . ex:Cell a owl:Class .'
ORG = 'https://example.org/followup/Org'
CLASS = 'https://example.org/followup/Organization'


def run(args):
    if args.allow_qa_writes:
        for url in (args.url, args.native_url):
            u = urlsplit(url)
            if u.hostname != '127.0.0.1' or u.port in (18080, 18081, 18084):
                raise ValueError('Write tests require explicit disposable loopback QA ports')
    spec = importlib.util.spec_from_file_location('client', args.client_script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    def connect(url, modern):
        c = module.Client(url, modern=modern)
        if modern:
            c.rpc('server/discover')
        else:
            c.rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'followup-regression', 'version': '1'}})
            c.rpc('notifications/initialized', notification=True)
        return c
    client = connect(args.url, not args.legacy)
    native = connect(args.native_url, False)
    evidence, checks = {}, []
    def call(c, name, params, error=False):
        r = c.rpc('tools/call', {'name': name, 'arguments': params})
        text = r['content'][0]['text']
        if c is client:
            assert bool(r.get('isError')) == error, (name, len(text), text[:512])
            if not args.legacy and text.startswith('{'):
                assert r.get('structuredContent') == json.loads(text), name
        return r
    def payload(c, name, params, error=False):
        r = call(c, name, params, error)
        return json.loads(r['content'][0]['text'])
    def snapshot():
        p = payload(native, 'onto_query', {'query': 'SELECT ?s ?p ?o ?g WHERE { { ?s ?p ?o } UNION { GRAPH ?g { ?s ?p ?o } } }'})
        return sorted(json.dumps(row, sort_keys=True) for row in p['results'])
    def convert(content, source='turtle', target='ntriples', error=False):
        return payload(client, 'onto_convert', {'inline_content': content, 'from': source, 'to': target}, error)
    try:
        tools = {t['name']: t for t in client.tools()}
        assert 'inline_content' in tools['onto_convert']['inputSchema']['properties']
        assert 'path' not in tools['onto_convert']['inputSchema'].get('required', [])
        # Path mode may write a server output: the full tool remains non-read-only.
        if not args.legacy:
            assert tools['onto_convert']['annotations']['readOnlyHint'] is False
        before = snapshot()
        counts = {}
        for target in ('turtle', 'ntriples', 'rdfxml', 'nquads', 'trig', 'jsonld', 'ofn'):
            p = convert(TTL, target=target)
            assert p['execution_scope'] == 'request' and p['persistent_store_modified'] is False
            assert p['content'] and '/request-' not in p['content'] and 'file:' not in p['content']
            if target == 'ofn':
                assert 'constructs_not_exported' in p and 'constructs_not_round_trippable' in p
            counts[target] = len(p['content'])
            # Exercise every source parser against a complete document emitted
            # by the same pinned engine. Errors must not be confused with RDF.
            back = convert(p['content'], source=target, target='nquads')
            assert '<https://example.org/Cell>' in back['content']
        checks.append('all_seven_inline_formats_both_directions')
        dataset = '<urn:s> <urn:p> "value" <urn:g> .\n<urn:s> <urn:p> "default" .'
        tri = convert(dataset, 'nquads', 'trig')
        back = convert(tri['content'], 'trig', 'nquads')
        assert '<urn:g>' in back['content'] and '"default"' in back['content']
        checks.append('dataset_graph_names_preserved_for_dataset_outputs')
        loss = convert('<urn:s> <urn:p> _:b . _:b <http://www.w3.org/1999/02/22-rdf-syntax-ns#first> <urn:o> .', target='ofn')
        assert loss['constructs_not_exported'] and loss['exports_fewer_triples_than_read']
        checks.append('ofn_unexported_construct_diagnostics_retained')
        for content, source in [('<#Cell> <urn:p> "v" .', 'turtle'),
                                ('<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:ex="https://e/"><rdf:Description rdf:about="#Cell"><ex:p>v</ex:p></rdf:Description></rdf:RDF>', 'rdfxml'),
                                ('{"@id":"#Cell","https://e/p":"v"}', 'jsonld'),
                                ('<file:///data/a> <urn:p> "v" .', 'ntriples')]:
            p = convert(content, source, error=True)
            assert 'file IRIs' in p['error'], p['error']
            assert '/request-' not in p['error']
        assert 'https://e/document#Cell' in convert('@base <https://e/document> . <#Cell> <urn:p> "v" .')['content']
        checks.append('relative_and_file_IRIs_rejected_explicit_base_accepted')
        for content in ('not RDF', '<urn:s> <urn:p> "unterminated'):
            p = convert(content, error=True)
            assert 'error' in p and '/request-' not in p['error']
        payload(client, 'onto_convert', {'inline_content': 'x' * (1048576 + 1), 'from': 'turtle', 'to': 'turtle'}, True)
        payload(client, 'onto_convert', {'inline_content': TTL, 'path': '/data/input.ttl', 'from': 'turtle', 'to': 'turtle'}, True)
        payload(client, 'onto_convert', {'inline_content': TTL, 'from': 'turtle', 'to': 'turtle', 'output': '/data/unsafe.ttl'}, True)
        checks.append('invalid_oversize_ambiguous_and_output_options_rejected')
        assert snapshot() == before
        evidence['conversion_shared_store_unchanged'] = True
        evidence['output_characters'] = counts
        if args.allow_qa_writes:
            # All mutations below are confined to the explicit disposable backend.
            payload(native, 'onto_policy_register', {'name': 'followup_deny_org', 'effect': 'deny', 'condition': 'ASK { FILTER(false) }'})
            payload(native, 'onto_clear', {})
            triples = [f'<https://example.org/followup/item/{i}> a <urn:Product> .' for i in range(2100)]
            triples += [f'<{ORG}> a <{CLASS}> .', f'<{CLASS}> a <http://www.w3.org/2002/07/owl#Class> .']
            payload(native, 'onto_load', {'turtle': '\n'.join(triples)})
            payload(native, 'onto_policy_register', {'name': 'followup_deny_org', 'effect': 'deny', 'condition': f'ASK {{ {{target}} a <{CLASS}> }}'})
            qa_before = snapshot()
            raw = call(native, 'onto_clear', {})
            original = json.loads(raw['content'][0]['text'])['error']
            assert len(original.encode()) > 8192
            native_report = json.loads(original.removeprefix('policy_denied: '))
            summary_result = call(client, 'onto_clear', {}, True)
            p = json.loads(summary_result['content'][0]['text'])
            summary = p['policy_summary']
            targets = {r['target'] for r in native_report['rule_results']}
            blocked = {r['target'] for r in native_report['rule_results'] if r['effect'] == 'deny' and r['fired']}
            assert summary['evaluated_targets'] == len(targets)
            assert summary['blocked_targets'] == len(blocked) == 1
            assert summary['rule_evaluations'] == len(native_report['rule_results'])
            assert summary['original_error_sha256'] == hashlib.sha256(original.encode()).hexdigest()
            assert len(json.dumps(summary_result).encode()) < 8192
            assert snapshot() == qa_before
            evidence['policy'] = {'native_error_bytes': len(original.encode()), 'adapted_wire_result_bytes': len(json.dumps(summary_result).encode()), 'evaluated_targets': len(targets), 'blocked_targets': len(blocked), 'graph_unchanged': True}
            checks.append('large_policy_denial_summarized_without_data_deletion')
            repo = payload(client, 'onto_repo_list', {}, True)
            assert repo['error_code'] == 'repository_not_configured' and repo['configuration_required']
            checks.append('repo_configuration_guidance_no_cache_fallback')
            if args.data_dir:
                file = args.data_dir / 'followup-path.ttl'
                file.write_text('<#Cell> <urn:p> "file mode" .')
                r = call(client, 'onto_convert', {'path': '/data/followup-path.ttl', 'to': 'ntriples'})
                assert 'file:///data/followup-path.ttl#Cell' in r['content'][0]['text']
                checks.append('existing_file_mode_preserves_native_file_base')
        if args.work_root:
            assert not list(args.work_root.iterdir()), 'private worker directories remain'
        evidence['checks'] = checks
        args.output.write_text(json.dumps(evidence, indent=2) + '\n')
        print(json.dumps(evidence, indent=2), flush=True)
    finally:
        client.close()
        native.close()


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('url')
    p.add_argument('--native-url', required=True)
    p.add_argument('--legacy', action='store_true')
    p.add_argument('--allow-qa-writes', action='store_true')
    p.add_argument('--data-dir', type=Path)
    p.add_argument('--work-root', type=Path)
    p.add_argument('--client-script', type=Path, default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'))
    p.add_argument('--output', type=Path, required=True)
    run(p.parse_args())
