// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

use open_ontologies::{
    graph::GraphStore,
    policy::{self, PolicyRule},
    state::StateDb,
};
use std::{path::Path, sync::Arc};

fn fixture() -> (StateDb, Arc<GraphStore>) {
    let db = StateDb::open(Path::new(":memory:")).unwrap();
    let graph = Arc::new(GraphStore::new());
    graph
        .load_turtle(
            "<urn:org> a <urn:Organization> . <urn:free> a <urn:Person> .",
            None,
        )
        .unwrap();
    policy::register_rule(
        &db,
        &PolicyRule {
            name: "deny_org".into(),
            effect: "deny".into(),
            condition: "ASK { {target} a <urn:Organization> }".into(),
            description: None,
        },
    )
    .unwrap();
    graph.bind_policy_db(db.clone());
    (db, graph)
}

#[test]
fn decision_normalizes_targets_and_never_allows_errors() {
    let (db, g) = fixture();
    for t in ["urn:org", "<urn:org>"] {
        assert_eq!(
            policy::check_action(&db, &g, &[t.into()]).unwrap().verdict,
            "deny"
        );
    }
    for t in ["not a valid IRI", "<urn:org> } UNION { ?s ?p ?o"] {
        assert!(policy::check_action(&db, &g, &[t.into()]).is_err());
    }
    assert!(policy::check_action(&db, &g, &[]).is_err());
    policy::register_rule(
        &db,
        &PolicyRule {
            name: "broken".into(),
            effect: "deny".into(),
            condition: "ASK { broken syntax }".into(),
            description: None,
        },
    )
    .unwrap();
    assert!(policy::check_action(&db, &g, &["urn:free".into()]).is_err());
    assert!(g.load_turtle("<urn:free> <urn:p> 1 .", None).is_err());
    assert_eq!(g.triple_count(), 2);
}

#[test]
fn guarded_load_update_clear_and_lines_preserve_all_data_on_denial() {
    let (_db, g) = fixture();
    assert!(
        g.load_turtle("<urn:org> <urn:p> 1 . <urn:free> <urn:p> 2 .", None)
            .is_err()
    );
    assert!(g.sparql_update("DELETE WHERE { <urn:org> ?p ?o }").is_err());
    assert!(g.clear().is_err());
    assert!(g.sparql_update("CREATE GRAPH <urn:org>").is_err());
    assert!(g.load_nquads("<urn:org> <urn:p> 1 <urn:graph> .").is_err());
    assert_eq!(g.triple_count(), 2);
    assert_eq!(
        g.load_turtle("<urn:org> a <urn:Organization> .", None)
            .unwrap(),
        0
    );
    assert_eq!(g.load_turtle("<urn:free> <urn:p> 2 .", None).unwrap(), 1);
    assert_eq!(g.triple_count(), 3);
}

#[test]
fn line_format_parse_errors_are_atomic_too() {
    let (_db, g) = fixture();
    assert!(
        g.load_ntriples("<urn:free> <urn:p> <urn:o> .\ninvalid RDF")
            .is_err()
    );
    assert_eq!(g.triple_count(), 2);
}

#[test]
fn protected_blank_node_subjects_are_bound_as_terms() {
    let db = StateDb::open(Path::new(":memory:")).unwrap();
    let graph = GraphStore::new();
    graph
        .load_ntriples(
            "_:org <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <urn:Organization> .",
        )
        .unwrap();
    policy::register_rule(
        &db,
        &PolicyRule {
            name: "deny_org".into(),
            effect: "deny".into(),
            condition: "ASK { <{target}> a <urn:Organization> }".into(),
            description: None,
        },
    )
    .unwrap();
    graph.bind_policy_db(db);
    assert!(graph.clear().is_err());
    assert_eq!(graph.triple_count(), 1);
}

#[test]
fn target_independent_rules_accept_blank_node_mutations() {
    let db = StateDb::open(Path::new(":memory:")).unwrap();
    let graph = GraphStore::new();
    graph.load_ntriples("_:b <urn:p> <urn:o> .").unwrap();
    policy::register_rule(
        &db,
        &PolicyRule {
            name: "constant".into(),
            effect: "deny".into(),
            condition: "ASK { FILTER(false) }".into(),
            description: None,
        },
    )
    .unwrap();
    graph.bind_policy_db(db);
    graph.clear().unwrap();
    assert_eq!(graph.triple_count(), 0);
}

#[test]
fn persistent_store_denial_survives_reopening() {
    let dir = tempfile::tempdir().unwrap();
    let db = StateDb::open(&dir.path().join("state.db")).unwrap();
    let path = dir.path().join("rdf");
    {
        let graph = GraphStore::open_persistent(&path).unwrap();
        graph
            .load_turtle("<urn:org> a <urn:Organization> .", None)
            .unwrap();
        policy::register_rule(
            &db,
            &PolicyRule {
                name: "deny".into(),
                effect: "deny".into(),
                condition: "ASK { {target} a <urn:Organization> }".into(),
                description: None,
            },
        )
        .unwrap();
        graph.bind_policy_db(db.clone());
        assert!(
            graph
                .sparql_update("DELETE WHERE { <urn:org> ?p ?o }")
                .is_err()
        );
    }
    assert_eq!(
        GraphStore::open_persistent(&path).unwrap().triple_count(),
        1
    );
}
