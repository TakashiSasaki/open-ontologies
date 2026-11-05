// SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
// SPDX-License-Identifier: MIT
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

//! Bind complete RDF terms, never SPARQL fragments or portions of literals.
use oxigraph::model::{NamedNode, Term, Variable};
use oxigraph::sparql::{PreparedSparqlQuery, SparqlEvaluator};
use std::collections::BTreeMap;

pub type Bindings = BTreeMap<String, Term>;

pub fn iri(input: &str) -> anyhow::Result<NamedNode> {
    let input = input.trim();
    if input.starts_with('<') {
        match input.parse::<Term>()? {
            Term::NamedNode(n) => Ok(n),
            _ => anyhow::bail!("expected an absolute IRI"),
        }
    } else {
        Ok(NamedNode::new(input)?)
    }
}

pub fn term(input: &str) -> anyhow::Result<Term> {
    let input = input.trim();
    if input.starts_with(['<', '"', '_']) {
        Ok(input.parse::<Term>()?)
    } else {
        Ok(iri(input)?.into())
    }
}

pub fn bindings(values: &[(String, String)]) -> anyhow::Result<Bindings> {
    let mut out = Bindings::new();
    for (key, value) in values {
        Variable::new(key.clone())?;
        anyhow::ensure!(
            !matches!(term(value)?, Term::BlankNode(_)),
            "action bindings require an IRI or literal, not a blank-node label"
        );
        anyhow::ensure!(
            out.insert(key.clone(), term(value)?).is_none(),
            "duplicate binding {key}"
        );
    }
    Ok(out)
}

fn identifier(s: &str) -> bool {
    !s.is_empty()
        && s.chars()
            .enumerate()
            .all(|(i, c)| c == '_' || c.is_alphabetic() || (i > 0 && c.is_ascii_digit()))
}

fn known_placeholder(s: &str, values: &Bindings) -> bool {
    values.keys().any(|k| s.contains(&format!("{{{k}}}")))
}

/// Replace only whole-term placeholders. Comments remain comments. Partial
/// IRI/literal interpolation is rejected instead of changing term meaning.
pub fn render(template: &str, values: &Bindings, variables: bool) -> anyhow::Result<String> {
    anyhow::ensure!(
        !template.contains("?__oo_bound_") && !template.contains("$__oo_bound_"),
        "reserved binding variable in template"
    );
    let mut out = String::new();
    let mut i = 0;
    let bytes = template.as_bytes();
    while i < bytes.len() {
        let start = i;
        if bytes[i] == b'#' {
            while i < bytes.len() && bytes[i] != b'\n' {
                i += 1;
            }
            out.push_str(&template[start..i]);
            continue;
        }
        if bytes[i] == b'\'' || bytes[i] == b'"' {
            let q = bytes[i];
            let width = if bytes.get(i..i + 3) == Some(&[q, q, q]) {
                3
            } else {
                1
            };
            i += width;
            let mut closed = false;
            while i < bytes.len() {
                if bytes[i] == b'\\' {
                    i += 2;
                    continue;
                }
                if bytes
                    .get(i..i + width)
                    .is_some_and(|x| x.iter().all(|c| *c == q))
                {
                    i += width;
                    closed = true;
                    break;
                }
                i += 1;
            }
            anyhow::ensure!(closed, "unterminated literal in template");
            anyhow::ensure!(
                !known_placeholder(&template[start..i], values),
                "partial literal template is unsupported; bind the whole RDF literal"
            );
            out.push_str(&template[start..i]);
            continue;
        }
        let mut key = None;
        if bytes[i] == b'<' {
            if let Some(end) = template[i + 1..].find('>') {
                let end = i + 1 + end;
                if !template[i + 1..end].chars().any(char::is_whitespace) {
                    let inner = &template[i + 1..end];
                    if inner.starts_with('{')
                        && inner.ends_with('}')
                        && identifier(&inner[1..inner.len() - 1])
                    {
                        key = Some(inner[1..inner.len() - 1].to_string());
                    } else {
                        anyhow::ensure!(
                            !known_placeholder(inner, values),
                            "partial IRI template is unsupported"
                        );
                    }
                    i = end + 1;
                    if key.is_none() {
                        out.push_str(&template[start..i]);
                        continue;
                    }
                }
            }
        }
        if bytes[start] == b'{'
            && let Some(end) = template[start + 1..].find('}')
        {
            let end = start + 1 + end;
            if identifier(&template[start + 1..end]) {
                key = Some(template[start + 1..end].to_string());
                i = end + 1;
            }
        }
        if let Some(key) = key {
            if start > 0 {
                let prev = template[..start].chars().next_back().unwrap();
                anyhow::ensure!(
                    !prev.is_alphanumeric() && !"_:\\".contains(prev),
                    "placeholder is not a whole RDF term"
                );
            }
            if i < bytes.len() {
                let next = template[i..].chars().next().unwrap();
                anyhow::ensure!(
                    !next.is_alphanumeric() && !"_:\\".contains(next),
                    "placeholder is not a whole RDF term"
                );
            }
            let value = values
                .get(&key)
                .ok_or_else(|| anyhow::anyhow!("missing binding {key}"))?;
            if variables {
                let index = values.keys().position(|k| k == &key).unwrap();
                out.push_str(&format!("?__oo_bound_{index}"));
            } else {
                out.push_str(&value.to_string());
            }
            continue;
        }
        // Copy a full UTF-8 character, keeping byte indices on boundaries.
        i = start + template[start..].chars().next().unwrap().len_utf8();
        out.push_str(&template[start..i]);
    }
    Ok(out)
}

pub fn prepare(template: &str, values: &Bindings) -> anyhow::Result<PreparedSparqlQuery> {
    anyhow::ensure!(
        values.values().all(|v| !matches!(v, Term::BlankNode(_))),
        "blank-node parameters require an identity-aware query path"
    );
    let text = render(template, values, false)?;
    local_condition(&text)?;
    Ok(SparqlEvaluator::new().parse_query(&text)?)
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum TokenKind {
    Word,
    Iri,
    Literal,
    Symbol,
}
#[derive(Clone, Debug)]
pub struct Token {
    pub text: String,
    pub start: usize,
    pub end: usize,
    pub kind: TokenKind,
}

/// Lexical boundaries only; the actual query and RDF parsers remain authoritative.
pub fn tokens(text: &str) -> anyhow::Result<Vec<Token>> {
    let b = text.as_bytes();
    let mut i = 0;
    let mut out = Vec::new();
    while i < b.len() {
        if b[i].is_ascii_whitespace() {
            i += 1;
            continue;
        }
        if b[i] == b'#' {
            while i < b.len() && b[i] != b'\n' {
                i += 1;
            }
            continue;
        }
        let start = i;
        let kind;
        if b[i] == b'"' || b[i] == b'\'' {
            let q = b[i];
            let width = if b.get(i..i + 3) == Some(&[q, q, q]) {
                3
            } else {
                1
            };
            i += width;
            let mut closed = false;
            while i < b.len() {
                if b[i] == b'\\' {
                    i += 2;
                    continue;
                }
                if b.get(i..i + width)
                    .is_some_and(|s| s.iter().all(|c| *c == q))
                {
                    i += width;
                    closed = true;
                    break;
                }
                i += 1;
            }
            anyhow::ensure!(closed, "unterminated query literal");
            kind = TokenKind::Literal;
        } else if b[i] == b'<'
            && text[i + 1..]
                .find('>')
                .is_some_and(|n| !text[i + 1..i + 1 + n].chars().any(char::is_whitespace))
        {
            i += text[i + 1..].find('>').unwrap() + 2;
            kind = TokenKind::Iri;
        } else if b"{}()[];,<>=!|^&.+*/".contains(&b[i]) {
            i += 1;
            kind = TokenKind::Symbol;
        } else {
            while i < b.len()
                && !b[i].is_ascii_whitespace()
                && !b"{}()[];,<>\"'#=|^&+*/".contains(&b[i])
            {
                i += 1;
            }
            if i == start {
                i += 1;
            }
            kind = TokenKind::Word;
        }
        out.push(Token {
            text: text[start..i].into(),
            start,
            end: i,
            kind,
        });
    }
    Ok(out)
}

pub fn query_head(text: &str) -> anyhow::Result<Token> {
    let ts = tokens(text)?;
    let mut i = 0;
    while i < ts.len() {
        if ts[i].text.eq_ignore_ascii_case("PREFIX") {
            anyhow::ensure!(
                i + 2 < ts.len()
                    && ts[i + 1].text.ends_with(':')
                    && ts[i + 2].kind == TokenKind::Iri,
                "malformed PREFIX"
            );
            i += 3;
        } else if ts[i].text.eq_ignore_ascii_case("BASE") {
            anyhow::ensure!(
                i + 1 < ts.len() && ts[i + 1].kind == TokenKind::Iri,
                "malformed BASE"
            );
            i += 2;
        } else {
            return Ok(ts[i].clone());
        }
    }
    anyhow::bail!("missing query")
}

pub fn local_condition(text: &str) -> anyhow::Result<()> {
    anyhow::ensure!(
        !tokens(text)?
            .iter()
            .any(|t| t.kind == TokenKind::Word && t.text.eq_ignore_ascii_case("SERVICE")),
        "federated SERVICE is unsupported in transactional conditions"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use oxigraph::{sparql::QueryResults, store::Store};
    #[test]
    fn bare_and_bracketed_iri_are_the_same_term() {
        assert_eq!(term("urn:org").unwrap(), term("<urn:org>").unwrap());
        for bad in ["not an IRI", "<urn:x> } UNION { ?s ?p ?o", "<urn:open"] {
            assert!(iri(bad).is_err());
        }
    }
    #[test]
    fn ask_substitution_respects_term_identity_and_scope() {
        let store = Store::new().unwrap();
        store.update("INSERT DATA { <urn:org> a <urn:C> }").unwrap();
        let b = bindings(&[("target".into(), "urn:org".into())]).unwrap();
        for q in [
            "ASK { {target} a <urn:C> }",
            "PREFIX x: <urn:> ASK { <{target}> a x:C }",
            "ASK { FILTER EXISTS { {target} a <urn:C> } }",
        ] {
            assert!(matches!(
                prepare(q, &b).unwrap().on_store(&store).execute().unwrap(),
                QueryResults::Boolean(true)
            ));
        }
    }
    #[test]
    fn no_recursive_or_partial_substitution() {
        let b = bindings(&[("x".into(), "\"{x} } UNION { ?s ?p ?o\"".into())]).unwrap();
        let store = Store::new().unwrap();
        assert!(matches!(
            prepare("ASK { <urn:s> <urn:p> {x} }", &b)
                .unwrap()
                .on_store(&store)
                .execute()
                .unwrap(),
            QueryResults::Boolean(false)
        ));
        for q in [
            "ASK { <urn:{x}> ?p ?o }",
            "ASK { ?s ?p \"prefix-{x}\" }",
            "ASK { ex:{x} ?p ?o }",
        ] {
            assert!(render(q, &b, true).is_err());
        }
        assert!(prepare("ASK { ?s ?p {missing} }", &b).is_err());
    }
}
