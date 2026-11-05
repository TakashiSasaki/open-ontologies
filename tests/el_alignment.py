#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""EL/evidence/profile acceptance; live publication tests require disposable QA."""
import argparse
import importlib.util
import json
from pathlib import Path
import time
from urllib.parse import urlsplit, urlunsplit

def ontology(iri, strong=False):
    text = f'<{iri}> a <http://www.w3.org/2002/07/owl#Class>; <http://www.w3.org/2000/01/rdf-schema#label> "Observation" .'
    if strong:
        text += f' <{iri}> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:common:Parent> . <urn:common:p> a <http://www.w3.org/2002/07/owl#ObjectProperty>; <http://www.w3.org/2000/01/rdf-schema#domain> <{iri}>; <http://www.w3.org/2000/01/rdf-schema#range> <urn:common:Parent> .'
    return text

def run(args):
    spec = importlib.util.spec_from_file_location('client', args.client_script)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    c = module.Client(args.url); c.rpc('server/discover')
    n = None; core = None; checks = []; timings = {}
    def call(client, name, arguments, error=False):
        r = client.rpc('tools/call', {'name': name, 'arguments': arguments})
        p = json.loads(r['content'][0]['text'])
        if error is not None: assert bool(r.get('isError')) == error, (name, p)
        if client.modern: assert r.get('structuredContent') == p
        return r, p
    def value(name, arguments, error=False): return call(c, name, arguments, error)[1]
    try:
        if args.allow_qa_writes:
            assert args.native_url and args.data_dir
            for url in (args.url, args.native_url):
                u = urlsplit(url)
                assert u.scheme == 'http' and u.hostname == '127.0.0.1' and u.port not in (18080, 18081, 18084)
            n = module.Client(args.native_url, modern=False)
            n.rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'el-evidence-qa', 'version': '1'}})
            small = '<urn:A> a <http://www.w3.org/2002/07/owl#Class>; <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:B> . <urn:B> a <http://www.w3.org/2002/07/owl#Class>; <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:C> . <urn:C> a <http://www.w3.org/2002/07/owl#Class> .'
            document = '<urn:el:small> { ' + small + ' }\n<urn:el:large> {\n' + '\n'.join(f'<urn:fixture:{i}> <urn:p> {i} .' for i in range(10005)) + '\n}\n'
            file = args.data_dir / 'el-evidence.trig'
            with file.open('x') as stream: stream.write(document)
            assert call(n, 'onto_load', {'path': '/data/el-evidence.trig'})[1]['ok']
        before = value('onto_status', {})['triples_loaded']
        tools = c.tools(); assert len(tools) == 125
        assert sum(bool(t.get('annotations', {}).get('readOnlyHint')) for t in tools) == 38
        el = next(t for t in tools if t['name'] == 'onto_classify_el')
        assert el['annotations']['readOnlyHint'] is False
        assert {'ontology_turtle', 'graph_iri', 'max_seconds'} <= set(el['inputSchema']['properties'])
        inline = '<urn:A> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:B> . <urn:B> <http://www.w3.org/2000/01/rdf-schema#subClassOf> <urn:C> .'
        p = value('onto_classify_el', {'ontology_turtle': inline})
        assert p['analysis_complete'] and p['complete'] and p['subsumption_count'] >= 3 and not p['persistent_store_modified']
        p = value('onto_classify_el', {'ontology_turtle': '<urn:A> a <http://www.w3.org/2002/07/owl#Class>; <http://www.w3.org/2002/07/owl#complementOf> <urn:B> .'})
        assert p['analysis_complete'] and p['complete'] is False and p['incompleteness']
        checks.append('private_EL_and_native_incompleteness_preserved')
        if before > 10000:
            start = time.monotonic(); p = value('onto_classify_el', {}, True)
            assert p['application_error']['code'] == 'analysis_scope_required' and not p['analysis_complete']
            timings['large_EL_refusal_seconds'] = time.monotonic() - start
            assert timings['large_EL_refusal_seconds'] < 20
            checks.append('large_EL_refused_before_shared_classification')
        if args.allow_qa_writes:
            p = value('onto_classify_el', {'graph_iri': 'urn:el:small'})
            assert p['analysis_scope']['selected_rows'] == 5 and p['analysis_complete'] and p['complete']
            checks.append('exact_named_graph_EL')
        for strong in (False, True):
            p = value('onto_align', {'source': ontology('urn:source:A', strong), 'target': ontology('urn:target:B', strong), 'high_threshold': 0.8, 'dry_run': True})
            assert p['applied_count'] == 0 and p['auto_applied'] == []
            if strong:
                assert len(p['auto_apply_candidates']) == 1 and p['borderline_count'] == 0
            else:
                assert p['auto_apply_candidates'] == [] and p['borderline_count'] == 1
                assert p['borderline'][0]['requires_review'] and p['borderline'][0]['confidence'] == 1
        checks.append('alignment_evidence_gate_and_honest_dry_run_counts')
        r, p = call(c, 'onto_search', {'query': 'OO_TEST_MODEL_READINESS'}, None)
        if r.get('isError'):
            assert p['dependency'] in ('embedding_model', 'embedding_index', 'embeddings_feature') and p['application_error']['code'] in ('dependency_unavailable', 'feature_unavailable')
        checks.append('search_readiness_or_success_then_status')
        assert value('onto_status', {})['triples_loaded'] == before
        u = urlsplit(args.url)
        core = module.Client(urlunsplit((u.scheme, u.netloc, '/mcp/core', '', ''))); core.rpc('server/discover')
        core_names = {t['name'] for t in core.tools()}
        expected_core = set(json.loads(args.core_profile.read_text())['profiles']['core'])
        assert core_names == expected_core and len(core_names) == 17
        _, p = call(core, 'onto_status', {})
        assert p['tools'] == 125 and 'before frontend profile' in p['tool_count_scope']
        checks.append('core_17_and_full_125_with_explicit_status_scope')
        if args.allow_qa_writes:
            value('onto_align', {'source': ontology('urn:weak:source'), 'target': ontology('urn:weak:target'), 'high_threshold': 0.8, 'dry_run': False})
            assert value('onto_status', {})['triples_loaded'] == before
            checks.append('weak_evidence_live_request_publishes_no_triples')
            p = value('onto_align', {'source': ontology('urn:source:A', True), 'target': ontology('urn:target:B', True), 'dry_run': False})
            assert p['applied_count'] == 1 and p['auto_applied'][0]['applied']
            p = value('onto_query', {'query': 'ASK { <urn:source:A> <http://www.w3.org/2002/07/owl#equivalentClass> <urn:target:B> }'})
            assert p['result'] is True
            after_publication = value('onto_status', {})['triples_loaded']; assert after_publication == before + 1
            checks.append('strong_evidence_additive_native_publication')
            call(n, 'onto_policy_register', {'name': 'deny_qa_alignment', 'effect': 'deny', 'condition': 'ASK {}'})
            p = value('onto_align', {'source': ontology('urn:denied:source', True), 'target': ontology('urn:denied:target', True), 'dry_run': False}, True)
            assert 'policy_denied' in p['error'] and value('onto_status', {})['triples_loaded'] == after_publication
            checks.append('native_policy_guard_blocks_entire_alignment_publication')
        if args.work_root:
            assert not list(args.work_root.iterdir()); checks.append('private_workers_cleaned')
        report = {'checks': checks, 'timings': timings, 'tools': 125, 'read_only': 38, 'core_tools': 17,
                  'read_only_phase_shared_count_preserved': True, 'initial_triples': before,
                  'final_triples': value('onto_status', {})['triples_loaded'], 'qa_writes': args.allow_qa_writes}
        args.output.write_text(json.dumps(report, indent=2) + '\n'); print(json.dumps(report))
    finally:
        if core: core.close()
        if n: n.close()
        c.close()

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('url'); p.add_argument('--native-url'); p.add_argument('--data-dir', type=Path)
    p.add_argument('--allow-qa-writes', action='store_true'); p.add_argument('--work-root', type=Path)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--core-profile', type=Path, default=Path(__file__).resolve().parents[1] / 'deploy/core-profile.json')
    p.add_argument('--client-script', type=Path, default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'))
    run(p.parse_args())
