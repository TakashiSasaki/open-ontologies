// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
use open_ontologies::{
    dynamics::{self, ActionSchema, EffectSpec, Parameter},
    dynamics_bcplus::{self, ConcurrentStep, StaticCausalLaw},
    graph::GraphStore,
    policy::{self, PolicyRule},
    state::StateDb,
};
use std::{
    collections::BTreeMap,
    path::Path,
    sync::{Arc, Barrier},
};

fn db() -> StateDb {
    StateDb::open(Path::new(":memory:")).unwrap()
}
fn schema() -> ActionSchema {
    ActionSchema {
        name: "assign".into(),
        parameters: vec![
            Parameter {
                name: "target".into(),
                type_iri: None,
            },
            Parameter {
                name: "value".into(),
                type_iri: None,
            },
        ],
        preconditions: vec!["ASK { {target} a <urn:C> }".into()],
        effects: vec![EffectSpec::AddTriple {
            subject: "<{target}>".into(),
            predicate: "<urn:p>".into(),
            object: "{value}".into(),
        }],
        reversible: false,
        description: None,
        outcomes: vec![],
    }
}
fn ask(g: &GraphStore, q: &str) -> bool {
    serde_json::from_str::<serde_json::Value>(&g.sparql_select(q).unwrap()).unwrap()["result"]
        .as_bool()
        .unwrap()
}

#[test]
fn actual_apply_accepts_both_iri_forms_and_whole_literals() {
    for target in ["urn:s", "<urn:s>"] {
        let db = db();
        let g = Arc::new(GraphStore::new());
        g.load_turtle("<urn:s> a <urn:C> .", None).unwrap();
        let action = schema();
        let args = vec![
            ("target".into(), target.into()),
            ("value".into(), "\"SN-003\"".into()),
        ];
        assert!(action.applicable_checked(&g, &args).unwrap());
        assert_eq!(action.apply(&g, &db, &args).unwrap().triples_added, 1);
        assert!(ask(&g, "ASK { <urn:s> <urn:p> \"SN-003\" }"));
        assert_eq!(action.apply(&g, &db, &args).unwrap().triples_added, 0);
    }
}

#[test]
fn invalid_later_effect_cannot_leave_an_earlier_effect() {
    let db = db();
    let g = Arc::new(GraphStore::new());
    g.load_turtle("<urn:s> a <urn:C> .", None).unwrap();
    let mut action = schema();
    action.effects.push(EffectSpec::AddTriple {
        subject: "urn:s".into(),
        predicate: "\"literal predicate\"".into(),
        object: "urn:o".into(),
    });
    assert!(
        action
            .apply(
                &g,
                &db,
                &[
                    ("target".into(), "urn:s".into()),
                    ("value".into(), "urn:o".into())
                ]
            )
            .is_err()
    );
    assert_eq!(g.triple_count(), 1);
}

#[test]
fn concurrent_preconditions_allow_only_one_claim() {
    let db = db();
    let g = Arc::new(GraphStore::new());
    let mut action = schema();
    action.parameters.pop();
    action.preconditions = vec!["ASK { FILTER NOT EXISTS { {target} <urn:claimed> true } }".into()];
    action.effects = vec![EffectSpec::AddTriple {
        subject: "{target}".into(),
        predicate: "urn:claimed".into(),
        object: "\"true\"^^<http://www.w3.org/2001/XMLSchema#boolean>".into(),
    }];
    let barrier = Arc::new(Barrier::new(3));
    let mut threads = Vec::new();
    for _ in 0..2 {
        let (db, g, action, b) = (db.clone(), g.clone(), action.clone(), barrier.clone());
        threads.push(std::thread::spawn(move || {
            b.wait();
            action
                .apply(&g, &db, &[("target".into(), "urn:s".into())])
                .is_ok()
        }));
    }
    barrier.wait();
    assert_eq!(
        threads
            .into_iter()
            .filter(|t| t.thread().id() != std::thread::current().id())
            .map(|t| usize::from(t.join().unwrap()))
            .sum::<usize>(),
        1
    );
    assert_eq!(g.triple_count(), 1);
}

#[test]
fn rejected_tick_preserves_preexisting_triples_and_empty_graphs() {
    let db = db();
    let g = Arc::new(GraphStore::new());
    g.load_turtle("<urn:old> a <http://www.w3.org/2002/07/owl#Class> .", None)
        .unwrap();
    g.sparql_update("CREATE GRAPH <urn:empty>").unwrap();
    let mut a = schema();
    a.parameters.clear();
    a.preconditions.clear();
    a.effects = vec![
        EffectSpec::AddClass {
            iri: "urn:old".into(),
        },
        EffectSpec::AddClass {
            iri: "urn:new".into(),
        },
    ];
    dynamics::register(&db, &a).unwrap();
    dynamics_bcplus::register_invariant(
        &db,
        &StaticCausalLaw {
            name: "no_new".into(),
            ask_query: "ASK { FILTER NOT EXISTS { <urn:new> ?p ?o } }".into(),
            description: None,
        },
    )
    .unwrap();
    let r = dynamics_bcplus::apply_concurrent(
        &db,
        &g,
        &[ConcurrentStep {
            action_name: a.name,
            bindings: BTreeMap::new(),
        }],
    )
    .unwrap();
    assert_eq!(r.steps_applied, 0);
    assert_eq!(g.triple_count(), 1);
    assert!(
        g.named_graph_iris()
            .unwrap()
            .iter()
            .any(|s| s == "urn:empty" || s == "<urn:empty>")
    );
    assert!(ask(
        &g,
        "ASK { <urn:old> a <http://www.w3.org/2002/07/owl#Class> }"
    ));
}

#[test]
fn denied_ramification_does_not_publish_the_base_effect() {
    let db = db();
    let g = Arc::new(GraphStore::new());
    g.load_turtle("<urn:org> a <urn:Organization> . <urn:p> a <http://www.w3.org/2002/07/owl#SymmetricProperty> .",None).unwrap();
    policy::register_rule(
        &db,
        &PolicyRule {
            name: "protect_org".into(),
            effect: "deny".into(),
            condition: "ASK { {target} a <urn:Organization> }".into(),
            description: None,
        },
    )
    .unwrap();
    let mut a = schema();
    a.parameters.clear();
    a.preconditions.clear();
    a.effects = vec![EffectSpec::AddTriple {
        subject: "urn:free".into(),
        predicate: "urn:p".into(),
        object: "urn:org".into(),
    }];
    assert!(a.apply_with_ramification(&g, &db, &[], "owl-rl").is_err());
    assert_eq!(g.triple_count(), 2);
    assert!(!ask(&g, "ASK { <urn:free> <urn:p> <urn:org> }"));
}
