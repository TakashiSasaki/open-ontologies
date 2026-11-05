#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Audit regressions. MUTATES a disposable QA backend; never use production."""
import argparse
import datetime
import importlib.util
import json
from pathlib import Path

ORG='https://example.org/org/AcmeCorp'
C='https://schema.org/Organization'
P='https://schema.org/member'
RDF='http://www.w3.org/1999/02/22-rdf-syntax-ns#'
OWL='http://www.w3.org/2002/07/owl#'

def run(args):
    spec=importlib.util.spec_from_file_location('client',args.client_script)
    m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
    def connect(url,legacy):
        c=m.Client(url,modern=not legacy)
        if legacy:
            c.rpc('initialize',{'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'audit-regression','version':'1'}})
            c.rpc('notifications/initialized',notification=True)
        else:c.rpc('server/discover')
        return c
    c=connect(args.url,args.legacy);n=connect(args.native_url,True)
    checks=[];evidence={}
    def call(client,name,params,error=False):
        r=client.rpc('tools/call',{'name':name,'arguments':params})
        p=json.loads(r['content'][0]['text'])
        if client is c:
            assert bool(r.get('isError'))==error,(name,r)
            if not args.legacy:assert r['structuredContent']==p,(name,r)
        return p
    def native(name,p):return call(n,name,p)
    def adapted(name,p,error=False):return call(c,name,p,error)
    try:
        native('onto_load',{'turtle':f'<{ORG}> a <{C}> . <{C}> a <{OWL}Class> . <{P}> a <{RDF}Property> .'})
        native('onto_policy_register',{'name':'deny_org','effect':'deny','condition':f'ASK {{ {{target}} a <{C}> }}'})
        count=native('onto_status',{})['triples_loaded']
        for template in [f'ASK {{ {{target}} a <{C}> }}',f'PREFIX s: <https://schema.org/> ASK {{ <{{target}}> a s:Organization }}']:
            native('onto_policy_register',{'name':'deny_org','effect':'deny','condition':template})
            for value in [ORG,'<'+ORG+'>']:
                p=adapted('onto_policy_check',{'target_iris':[value]});assert p['verdict']=='deny' and not p['authorization_enforced']
        for value in ['not a valid IRI','<'+ORG+'> } UNION { ?s ?p ?o','<https://example.org/unclosed']:
            p=adapted('onto_policy_check',{'target_iris':[value]},True);assert p['verdict']=='indeterminate'
        p=adapted('onto_policy_check',{'target_iris':[]},True);assert p['verdict']=='indeterminate'
        native('onto_policy_register',{'name':'broken_query','effect':'deny','condition':'ASK { clearly broken }'})
        p=adapted('onto_policy_check',{'target_iris':['urn:unmatched']},True);assert p['verdict']=='indeterminate'
        native('onto_policy_register',{'name':'broken_query','effect':'deny','condition':'ASK { FILTER(false) }'})
        assert adapted('onto_policy_check',{'target_iris':['urn:unmatched']})['verdict']=='allow'
        checks.append('policy_bare_bracketed_prefixed_invalid_empty_injection_query_error')
        for target,value in [('targetClass',C),('targetNode',ORG),('targetSubjectsOf',P),('targetObjectsOf',P)]:
            shapes=f'@prefix sh: <http://www.w3.org/ns/shacl#> . <urn:S> a sh:NodeShape ; sh:{target} <{value}> ; sh:property [sh:path <https://schema.org/seriallNumber>] .'
            p=adapted('onto_shacl_check',{'shapes':shapes,'inline':True});assert not p['ok'] and p['shape_count']==2 and any(x['kind']=='missing_path' for x in p['issues']),p
        for path in [f'<{P}>',f'[sh:inversePath <{P}>]',f'(<{P}> <{P}>)',f'[sh:alternativePath (<{P}> <{P}>)]']:
            shapes=f'@prefix sh: <http://www.w3.org/ns/shacl#> . <urn:S> a sh:PropertyShape ; sh:path {path} .'
            p=adapted('onto_shacl_check',{'shapes':shapes,'inline':True});assert p['ok'] and p['reference_check_complete'],p
        nested='@prefix sh: <http://www.w3.org/ns/shacl#> . <urn:S> a sh:NodeShape ; sh:targetNode <urn:n> ; sh:node [sh:class <urn:missing>] .'
        assert not adapted('onto_shacl_check',{'shapes':nested,'inline':True})['ok']
        cycle='@prefix sh: <http://www.w3.org/ns/shacl#> . <urn:S> a sh:PropertyShape ; sh:path _:p . _:p sh:inversePath _:p .'
        p=adapted('onto_shacl_check',{'shapes':cycle,'inline':True});assert not p['ok'] and not p['reference_check_complete']
        checks.append('shacl_all_four_targets_property_shape_nested_class_inverse_sequence_alternative_cycle')
        schema={'name':'audit_assign','parameters':[{'name':'target'}],'preconditions':[f'ASK {{ {{target}} a <{C}> }}'],'effects':[{'kind':'add_triple','subject':'{target}','predicate':P,'object':'urn:member'}],'reversible':False}
        native('onto_action_register',{'schema_json':json.dumps(schema)})
        info=adapted('onto_action_info',{'action_name':'audit_assign'});assert info['actions'][0]['schema']==schema or info['actions'][0]['schema']['preconditions']==schema['preconditions']
        for value in [ORG,'<'+ORG+'>']:
            p=adapted('onto_action_applicable',{'action_name':'audit_assign','bindings':{'target':value}});assert p['applicable'] and p['live_iri_binding_compatible'] is False
            p=adapted('onto_plan_validate',{'steps':[{'action_name':'audit_assign','bindings':{'target':value}}],'goal_facts':[['<'+ORG+'>','<'+P+'>','<urn:member>']]})
            assert p['valid'] and not p['unsatisfied_goals'] and not p['persistent_store_modified'],p
        assert native('onto_status',{})['triples_loaded']==count
        adapted('onto_action_applicable',{'action_name':'audit_assign','bindings':{'target':'not an IRI'}},True)
        adapted('onto_plan_validate',{'steps':[],'goal_facts':[['<urn:s>','<urn:p>','<urn:o> } UNION { ?s ?p ?o']]},True)
        goals=adapted('onto_plan_validate',{'steps':[],'goal_facts':[['<urn:s>','<urn:p>','"x"@en']]});assert goals['unsatisfied_goals']
        p=adapted('onto_plan_compile_pddl',{'action_names':['audit_assign']});assert not p['execution_ready'] and not p['translation_complete'] and 'untranslated term: a' not in p['translation_notes']
        assert 'http___www_w3_org_1999_02_22_rdf_syntax_ns_type' in p['domain']
        evidence['pddl_blockers']=p['execution_blockers']
        checks.append('action_details_safe_bindings_private_plan_goals_pddl_type_and_execution_guard')
        adapted('onto_query',{'query':'clearly invalid SPARQL'},True)
        negative=adapted('onto_shacl',{'ontology_turtle':'<urn:s> <urn:p> 1 .','shapes':'@prefix sh: <http://www.w3.org/ns/shacl#> . <urn:S> a sh:NodeShape ; sh:targetNode <urn:s> ; sh:property [sh:path <urn:absent> ; sh:minCount 1] .','inline':True})
        assert negative.get('conforms') is False
        checks.append('isError_for_failures_without_relabeling_valid_negative_conformance')
        a=f'<urn:source:Organization> a <{OWL}Class> ; <http://www.w3.org/2000/01/rdf-schema#label> "Organization" .'
        b=f'<urn:target:Organization> a <{OWL}Class> ; <http://www.w3.org/2000/01/rdf-schema#label> "Organization" .'
        p=adapted('onto_align',{'source':a,'target':b,'dry_run':True});assert p['applied_count']==0 and p['auto_applied']==[] and '0 applied' in p['summary_for_review'] and 'confidence_semantics' in p
        p=adapted('onto_temporal_query',{'pattern':'?s ?p ?o'});assert p['scope']['default_graph_included'] is False
        p=adapted('onto_status',{});assert 'capabilities' in p and 'status_scope' in p
        checks.append('dry_run_alignment_named_graph_scope_optional_readiness')
        assert native('onto_status',{})['triples_loaded']==count
        report={'checked_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'url':args.url,'native_url':args.native_url,'shared_triples_unchanged':count,'checks':checks,'evidence':evidence}
        args.output.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
    finally:c.close();n.close()

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('url');p.add_argument('--native-url',required=True);p.add_argument('--allow-qa-writes',action='store_true',required=True);p.add_argument('--legacy',action='store_true');p.add_argument('--client-script',type=Path,default=Path('/data/mcp-compat-proxy/main/scripts/check-http.py'));p.add_argument('--output',type=Path,required=True);run(p.parse_args())
