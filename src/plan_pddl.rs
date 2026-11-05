// SPDX-FileCopyrightText: 2026 Fabio Rovai
// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

//! A matched PDDL domain/problem for deterministic finite RDF triple actions.
//! Positive ASK basic triple patterns are supported. Arbitrary SPARQL, OWL
//! entailment, policy/invariant logic, blank-node parameters and stochastic
//! actions are not silently approximated. Notes make the artifact unready.
use crate::{
    dynamics::{ActionSchema, EffectSpec},
    graph::GraphStore,
    rdf_bindings::{self, Bindings},
};
use oxigraph::model::Term;
use std::collections::{BTreeMap, BTreeSet};

pub type Triple = (String, String, String);
#[derive(Clone, Debug)]
pub struct CompiledPddl {
    pub domain: String,
    pub problem: String,
    pub translation_notes: Vec<String>,
    pub translation_complete: bool,
    pub execution_ready: bool,
    /// PDDL symbol -> canonical RDF term (including literal quotes/datatype).
    pub symbols: BTreeMap<String, String>,
    /// PDDL operator -> native action name; parameters retain declaration order.
    pub action_symbols: BTreeMap<String, String>,
    pub parameter_symbols: BTreeMap<String, Vec<(String, String)>>,
}
fn symbol(prefix: &str, input: &str) -> String {
    let mut s = format!("{prefix}_");
    for b in input.as_bytes() {
        use std::fmt::Write;
        write!(s, "{b:02x}").unwrap();
    }
    s
}
#[derive(Clone)]
enum Position {
    Var(String),
    Constant(String),
}
type Atom = [Position; 3];
#[derive(Default)]
struct Compiler {
    symbols: BTreeMap<String, String>,
    types: BTreeMap<String, &'static str>,
    actions: BTreeMap<String, String>,
    parameters: BTreeMap<String, Vec<(String, String)>>,
    notes: BTreeSet<String>,
}
impl Compiler {
    fn constant(&mut self, raw: &str, iri_only: bool) -> anyhow::Result<String> {
        let term = rdf_bindings::term(raw)?;
        anyhow::ensure!(
            !matches!(term, Term::BlankNode(_)),
            "blank-node identity is outside the executable fragment"
        );
        anyhow::ensure!(
            !iri_only || matches!(term, Term::NamedNode(_)),
            "subject/predicate requires an IRI"
        );
        let canonical = term.to_string();
        let key = symbol("t", &canonical);
        self.types.insert(
            key.clone(),
            if matches!(term, Term::Literal(_)) {
                "literal"
            } else {
                "iri"
            },
        );
        self.symbols.insert(key.clone(), canonical);
        Ok(key)
    }
    fn effect_position(
        &mut self,
        text: &str,
        params: &BTreeSet<String>,
        iri_only: bool,
    ) -> anyhow::Result<Position> {
        let t = text.trim();
        if let Some(name) = t
            .strip_prefix("<{")
            .and_then(|s| s.strip_suffix("}>"))
            .or_else(|| t.strip_prefix('{').and_then(|s| s.strip_suffix('}')))
        {
            anyhow::ensure!(params.contains(name), "undeclared action parameter {name}");
            return Ok(Position::Var(name.into()));
        }
        anyhow::ensure!(
            !params.iter().any(|p| t.contains(&format!("{{{p}}}"))),
            "partial effect template is unsupported"
        );
        Ok(Position::Constant(self.constant(t, iri_only)?))
    }
    fn ask(&mut self, text: &str, params: &BTreeSet<String>) -> anyhow::Result<Vec<Atom>> {
        let namespace = "urn:open-ontologies:pddl:parameter:";
        anyhow::ensure!(
            !text.contains(namespace),
            "reserved compiler parameter namespace"
        );
        let values: Bindings = params
            .iter()
            .enumerate()
            .map(|(i, p)| Ok((p.clone(), rdf_bindings::term(&format!("{namespace}{i}"))?)))
            .collect::<anyhow::Result<_>>()?;
        let rendered = rdf_bindings::render(text, &values, false)?;
        let _ = rdf_bindings::prepare(text, &values)?;
        let head = rdf_bindings::query_head(&rendered)?;
        anyhow::ensure!(
            head.text.eq_ignore_ascii_case("ASK"),
            "only ASK triple-pattern preconditions are supported"
        );
        let tokens = rdf_bindings::tokens(&rendered)?;
        let mut pos = tokens.iter().position(|t| t.start == head.start).unwrap() + 1;
        if tokens
            .get(pos)
            .is_some_and(|t| t.text.eq_ignore_ascii_case("WHERE"))
        {
            pos += 1;
        }
        anyhow::ensure!(
            tokens.get(pos).is_some_and(|t| t.text == "{")
                && tokens.last().is_some_and(|t| t.text == "}"),
            "unsupported ASK dataset/modifier"
        );
        let body = &rendered[tokens[pos].end..tokens.last().unwrap().start];
        let mut turtle = String::new();
        let mut p = 0;
        while tokens[p].start < head.start {
            if tokens[p].text.eq_ignore_ascii_case("PREFIX") {
                turtle.push_str(&format!(
                    "@prefix {} {} .\n",
                    tokens[p + 1].text,
                    tokens[p + 2].text
                ));
                p += 3;
            } else {
                turtle.push_str(&format!("@base {} .\n", tokens[p + 1].text));
                p += 2;
            }
        }
        turtle.push_str(body);
        if rdf_bindings::tokens(body)?
            .last()
            .is_some_and(|t| t.text != ".")
        {
            turtle.push_str("\n.\n");
        }
        let triples = GraphStore::parse_triples_ordered(&turtle, "turtle")?;
        let reverse = values
            .iter()
            .map(|(p, t)| (t.to_string(), p.clone()))
            .collect::<BTreeMap<_, _>>();
        triples
            .into_iter()
            .map(|(s, p, o)| {
                let mut atom = Vec::new();
                for (i, t) in [s, p, o].iter().enumerate() {
                    atom.push(if let Some(name) = reverse.get(t) {
                        Position::Var(name.clone())
                    } else {
                        Position::Constant(self.constant(t, i < 2)?)
                    });
                }
                Ok(atom.try_into().ok().unwrap())
            })
            .collect()
    }
    fn atom(atom: &Atom) -> String {
        let terms = atom
            .iter()
            .map(|p| match p {
                Position::Var(v) => format!("?{}", symbol("v", v)),
                Position::Constant(c) => c.clone(),
            })
            .collect::<Vec<_>>();
        format!("(triple {})", terms.join(" "))
    }
    fn action(&mut self, schema: &ActionSchema) -> anyhow::Result<String> {
        anyhow::ensure!(
            schema.outcomes.is_empty(),
            "stochastic outcomes are unsupported"
        );
        let params = schema
            .parameters
            .iter()
            .map(|p| p.name.clone())
            .collect::<BTreeSet<_>>();
        anyhow::ensure!(
            params.len() == schema.parameters.len(),
            "duplicate parameters"
        );
        let mut pre = Vec::new();
        for q in &schema.preconditions {
            pre.extend(self.ask(q, &params)?);
        }
        let mut effects = Vec::new();
        for effect in &schema.effects {
            let (s, p, o, add) = match effect {
                EffectSpec::AddTriple {
                    subject,
                    predicate,
                    object,
                } => (subject.as_str(), predicate.as_str(), object.as_str(), true),
                EffectSpec::RemoveTriple {
                    subject,
                    predicate,
                    object,
                } => (subject.as_str(), predicate.as_str(), object.as_str(), false),
                EffectSpec::AddClass { iri } => (
                    iri.as_str(),
                    "http://www.w3.org/1999/02/22-rdf-syntax-ns#type",
                    "http://www.w3.org/2002/07/owl#Class",
                    true,
                ),
            };
            effects.push((
                [
                    self.effect_position(s, &params, true)?,
                    self.effect_position(p, &params, true)?,
                    self.effect_position(o, &params, false)?,
                ],
                add,
            ));
        }
        // A parameter used as a subject or predicate can never be a literal.
        let mut iri_params = BTreeSet::new();
        for atom in pre.iter().chain(effects.iter().map(|(a, _)| a)) {
            for p in &atom[..2] {
                if let Position::Var(v) = p {
                    iri_params.insert(v.clone());
                }
            }
        }
        let name = symbol("a", &schema.name);
        anyhow::ensure!(!self.actions.contains_key(&name), "duplicate action name");
        self.actions.insert(name.clone(), schema.name.clone());
        self.parameters.insert(
            name.clone(),
            schema
                .parameters
                .iter()
                .map(|p| (symbol("v", &p.name), p.name.clone()))
                .collect(),
        );
        let parameters = schema
            .parameters
            .iter()
            .map(|p| {
                format!(
                    "?{} - {}",
                    symbol("v", &p.name),
                    if iri_params.contains(&p.name) {
                        "iri"
                    } else {
                        "rdf_term"
                    }
                )
            })
            .collect::<Vec<_>>()
            .join(" ");
        let pre = pre.iter().map(Self::atom).collect::<Vec<_>>().join(" ");
        // Preserve native delete-wins semantics even when different parameter
        // names alias the same RDF term after grounding. Equality conditions
        // depend only on operator arguments, never on an intermediate state.
        let mut rendered = Vec::new();
        for (atom, add) in &effects {
            let value = Self::atom(atom);
            if !*add {
                rendered.push(format!("(not {value})"));
                continue;
            }
            let mut guards = Vec::new();
            let mut always_deleted = false;
            for (delete, is_add) in &effects {
                if *is_add {
                    continue;
                }
                let mut differences = Vec::new();
                let mut disjoint = false;
                for (a, b) in atom.iter().zip(delete) {
                    let term = |p: &Position| match p {
                        Position::Var(v) => format!("?{}", symbol("v", v)),
                        Position::Constant(c) => c.clone(),
                    };
                    match (a, b) {
                        (Position::Constant(x), Position::Constant(y)) if x != y => {
                            disjoint = true;
                            break;
                        }
                        _ => {
                            let x = term(a);
                            let y = term(b);
                            if x != y {
                                differences.push(format!("(not (= {x} {y}))"));
                            }
                        }
                    }
                }
                if disjoint {
                    continue;
                }
                if differences.is_empty() {
                    always_deleted = true;
                    break;
                }
                guards.push(format!("(or {})", differences.join(" ")));
            }
            if always_deleted {
                continue;
            }
            rendered.push(if guards.is_empty() {
                value
            } else {
                format!("(when (and {}) {value})", guards.join(" "))
            });
        }
        let rendered = rendered.join(" ");
        Ok(format!(
            "  (:action {name}\n    :parameters ({parameters})\n    :precondition (and {pre})\n    :effect (and {rendered}))"
        ))
    }
    fn facts(&mut self, facts: &[Triple], label: &str) -> Vec<String> {
        facts
            .iter()
            .filter_map(|(s, p, o)| {
                let atom: anyhow::Result<Atom> = (|| {
                    Ok([
                        Position::Constant(self.constant(s, true)?),
                        Position::Constant(self.constant(p, true)?),
                        Position::Constant(self.constant(o, false)?),
                    ])
                })();
                match atom {
                    Ok(a) => Some(Self::atom(&a)),
                    Err(e) => {
                        self.notes.insert(format!("{label}: {e}"));
                        None
                    }
                }
            })
            .collect()
    }
}

/// Compile the pair together so every constant used by an action or problem
/// is declared once. Older independent domain/problem helpers could not do so.
pub fn compile(
    domain_name: &str,
    schemas: &[ActionSchema],
    init: &[Triple],
    goals: &[Triple],
) -> CompiledPddl {
    let mut c = Compiler::default();
    let mut actions = Vec::new();
    for s in schemas {
        match c.action(s) {
            Ok(a) => actions.push(a),
            Err(e) => {
                c.notes.insert(format!("action {}: {e}", s.name));
            }
        }
    }
    let init = c.facts(init, "initial state");
    let goals = c.facts(goals, "goal");
    let declarations = c
        .types
        .iter()
        .map(|(s, t)| format!("{s} - {t}"))
        .collect::<Vec<_>>()
        .join("\n    ");
    let domain_symbol = symbol("d", domain_name);
    let domain = format!(
        "(define (domain {domain_symbol})\n  (:requirements :strips :typing :equality :conditional-effects :disjunctive-preconditions :negative-preconditions)\n  (:types iri literal - rdf_term rdf_term - object)\n  (:constants {declarations})\n  (:predicates (triple ?s - iri ?p - iri ?o - rdf_term))\n{}\n)\n",
        actions.join("\n")
    );
    let problem = format!(
        "(define (problem rdf_problem)\n  (:domain {domain_symbol})\n  (:init {})\n  (:goal (and {}))\n)\n",
        init.join(" "),
        goals.join(" ")
    );
    CompiledPddl {
        domain,
        problem,
        translation_complete: c.notes.is_empty(),
        execution_ready: c.notes.is_empty(),
        translation_notes: c.notes.into_iter().collect(),
        symbols: c.symbols,
        action_symbols: c.actions,
        parameter_symbols: c.parameters,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::dynamics::Parameter;
    fn action() -> ActionSchema {
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
            preconditions: vec!["PREFIX x: <urn:> ASK { {target} a x:C }".into()],
            effects: vec![EffectSpec::AddTriple {
                subject: "{target}".into(),
                predicate: "urn:p".into(),
                object: "{value}".into(),
            }],
            reversible: false,
            description: None,
            outcomes: vec![],
        }
    }
    #[test]
    fn type_shorthand_equals_explicit_iri() {
        let a = action();
        let mut b = a.clone();
        b.preconditions[0] = b.preconditions[0]
            .replace(" a ", " <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> ");
        let x = compile("d", &[a], &[], &[]);
        let y = compile("d", &[b], &[], &[]);
        assert!(x.translation_complete);
        assert_eq!(x.domain, y.domain);
    }
    #[test]
    fn symbols_are_injective_and_reversible() {
        let mut a = action();
        a.name = "Case-A".into();
        let mut b = a.clone();
        b.name = "case_a".into();
        let facts = vec![("urn:A-B".into(), "urn:p".into(), "urn:a_b".into())];
        let c = compile("d", &[a, b], &facts, &[]);
        assert!(c.translation_complete);
        assert_eq!(c.action_symbols.len(), 2);
        assert!(c.symbols.values().any(|v| v == "<urn:A-B>"));
        assert!(c.symbols.values().any(|v| v == "<urn:a_b>"));
        for (k, v) in &c.symbols {
            assert_eq!(k, &symbol("t", v));
        }
    }
    #[test]
    fn literals_have_declared_symbols_and_do_not_escape_pddl() {
        let facts = vec![(
            "urn:s".into(),
            "urn:p".into(),
            "\"a ) ( injected\"@en".into(),
        )];
        let c = compile("d", &[action()], &facts, &facts);
        assert!(c.execution_ready);
        assert!(c.domain.contains(" - literal"));
        assert!(!c.problem.contains("injected"));
        assert!(c.symbols.values().any(|v| v.contains("injected")));
    }
    #[test]
    fn unsupported_sparql_and_blank_nodes_are_never_execution_ready() {
        for condition in [
            "SELECT ?s WHERE { ?s ?p ?o }",
            "ASK { ?s ?p ?o FILTER(?o>1) }",
            "ASK { GRAPH <urn:g> { <urn:s> <urn:p> <urn:o> } }",
        ] {
            let mut a = action();
            a.preconditions = vec![condition.into()];
            let c = compile("d", &[a], &[], &[]);
            assert!(!c.execution_ready && !c.translation_notes.is_empty());
        }
        let c = compile(
            "d",
            &[],
            &[("_:b".into(), "urn:p".into(), "urn:o".into())],
            &[],
        );
        assert!(!c.execution_ready);
    }
    #[test]
    fn declarations_cover_every_problem_constant_once() {
        let f = vec![("urn:s".into(), "urn:p".into(), "\"x\"".into())];
        let c = compile("d", &[action()], &f, &f);
        assert!(c.execution_ready);
        for k in c.symbols.keys() {
            assert_eq!(c.domain.matches(&format!("{k} - ")).count(), 1);
        }
    }

    #[test]
    fn different_parameter_names_can_alias_a_deleted_triple() {
        let mut a = action();
        a.effects = vec![
            EffectSpec::AddTriple {
                subject: "{target}".into(),
                predicate: "urn:p".into(),
                object: "urn:o".into(),
            },
            EffectSpec::RemoveTriple {
                subject: "{value}".into(),
                predicate: "urn:p".into(),
                object: "urn:o".into(),
            },
        ];
        let c = compile("d", &[a], &[], &[]);
        assert!(c.execution_ready);
        assert!(c.domain.contains("(when (and (or (not (= ?v_"));
    }
}
