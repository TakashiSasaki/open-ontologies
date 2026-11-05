#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Read-only/private acceptance; explicit QA option seeds disposable named graphs."""
import argparse
import importlib.util
import json
from pathlib import Path
import time
from urllib.parse import urlsplit

def run(args):
    spec = importlib.util.spec_from_file_location('client', args.client_script)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    c = module.Client(args.url)
    c.rpc('server/discover')
    checks = []
    def call(name, arguments, error=False):
        r = c.rpc('tools/call', {'name': name, 'arguments': arguments})
        p = json.loads(r['content'][0]['text'])
        assert bool(r.get('isError')) == error, (name, p)
        assert r.get('structuredContent') == p
        if error and 'error_code' in p:
            assert p['application_error']['code'] == p['error_code']
        return p
    try:
        if args.allow_qa_writes:
            for url in (args.url, args.native_url):
                u = urlsplit(url)
                assert u.scheme == 'http' and u.hostname == '127.0.0.1' and u.port not in (18080, 18081, 18084)
            assert args.data_dir and args.native_url
            document = '<urn:scope:small> { <urn:A> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:B> . <urn:B> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:A> . <urn:S> <urn:p> [<urn:q> "value"] . }\n'
            document += '<urn:scope:large> {\n' + '\n'.join(f'<urn:fixture:{i}> <urn:p> {i} .' for i in range(10005)) + '\n}\n'
            file = args.data_dir / 'scalability.trig'
            with file.open('x') as stream: stream.write(document)
            n = module.Client(args.native_url, modern=False)
            try:
                n.rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'bounded-qa-fixture', 'version': '1'}})
                n.rpc('notifications/initialized', notification=True)
                r = n.rpc('tools/call', {'name': 'onto_load', 'arguments': {'path': '/data/scalability.trig'}})
                assert json.loads(r['content'][0]['text'])['ok']
            finally: n.close()
        before = call('onto_status', {})['triples_loaded']
        tools = c.tools()
        properties = next(t for t in tools if t['name'] == 'onto_defects')['inputSchema']['properties']
        assert 'graph_iri' in properties and properties['max_seconds']['maximum'] == 20
        inline = '<urn:A> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:B> . <urn:B> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:A> .'
        p = call('onto_defects', {'ontology_turtle': inline, 'max_seconds': 5})
        assert p['complete'] and p['defect_count'] >= 2 and not p['persistent_store_modified']
        checks.append('private_cycle_detected_with_complete_selected_scope')
        for bad in ({'graph_iri': 'urn:g> } UNION { ?s ?p ?o'}, {'max_seconds': 0}, {'ontology_turtle': '', 'graph_iri': 'urn:g'}):
            p = call('onto_defects', bad, error=True)
            assert p['error_code'] == 'invalid_arguments' and p['complete'] is False
        checks.append('invalid_scope_and_budget_fail_closed')
        if args.expect_large or args.allow_qa_writes:
            started = time.monotonic()
            p = call('onto_defects', {}, error=True)
            assert p['error_code'] == 'analysis_scope_required' and p['complete'] is False
            assert time.monotonic() - started < 20
            checks.append('large_shared_scope_refused_before_expensive_native_analysis')
        if args.allow_qa_writes:
            p = call('onto_defects', {'graph_iri': 'urn:scope:small'})
            assert p['complete'] and p['defect_count'] >= 2
            assert p['analysis_scope']['kind'] == 'named_graph_snapshot' and p['analysis_scope']['selected_rows'] == 4
            assert p['analysis_scope']['global_verdict'] is False
            p = call('onto_defects', {'graph_iri': 'urn:scope:empty'})
            assert p['complete'] and p['analysis_scope']['selected_rows'] == 0
            checks.append('exact_named_graph_blank_node_and_empty_scope')
            dense = '\n'.join(f'<urn:c{i}> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:c{(i+1)%1000}> .' for i in range(1000))
            started = time.monotonic()
            p = call('onto_defects', {'ontology_turtle': dense, 'max_seconds': 1}, error=True)
            assert p['error_code'] == 'analysis_deadline_exceeded' and p['complete'] is False and p['partial_results_available'] is False
            assert time.monotonic() - started < 5
            checks.append('real_native_deadline_returns_structured_incomplete_error')
        p = call('onto_crosswalk', {'code': 'OO_TEST_NO_CODE', 'source_system': 'ICD10'}, error=True)
        assert p['error_code'] == 'dependency_unavailable' and p['dependency'] == 'clinical_crosswalk'
        call('onto_convert', {'inline_content': 'not RDF', 'from': 'turtle', 'to': 'rdfxml'}, error=True)
        after = call('onto_status', {})['triples_loaded']
        assert before == after
        checks.append('missing_crosswalk_and_bad_rdf_are_errors_then_status_succeeds')
        if args.work_root:
            assert not list(args.work_root.iterdir())
            checks.append('private_workspaces_cleaned')
        report = {'checks': checks, 'shared_triples_before': before, 'shared_triples_after': after,
                  'shared_count_preserved': True, 'qa_writes': args.allow_qa_writes,
                  'tools': len(tools), 'read_only': sum(bool(t.get('annotations', {}).get('readOnlyHint')) for t in tools)}
        args.output.write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report))
    finally: c.close()

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('url'); p.add_argument('--native-url'); p.add_argument('--data-dir', type=Path)
    p.add_argument('--work-root', type=Path); p.add_argument('--allow-qa-writes', action='store_true')
    p.add_argument('--expect-large', action='store_true'); p.add_argument('--output', type=Path, required=True)
    p.add_argument('--client-script', type=Path, default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'))
    run(p.parse_args())
