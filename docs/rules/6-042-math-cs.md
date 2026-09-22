# Rules — Mathematics for Computer Science (MIT 6.042J)

Compressed rule catalog derived from the MIT 6.042J textbook
(Lehman/Leighton/Meyer, 2015). **This is our own derived work**: rule ids,
short forms (`premises ⊢ conclusion`) and one-line statements in our own words.
The licensed book text is **not** stored in this repository and never enters the
GitHub mirror (rule #190); the extractor keeps its artifacts in gitignored
`var/rules/6-042/` (epic #324).

- Machine-readable source of truth: `etc/rules/6-042/*.yml`
- Loader/validator: `internal/rules` (`Load`, `Validate`, `Executable`)
- Provenance (URL, edition, license, sha256): `etc/rules/6-042/index.yml`
- Extraction tool: `bin/rules/book.go` (`var/rules/6-042/`)

## How to read it

| Field | Meaning |
|-------|---------|
| `id` | `domain.family.name`, stable key |
| `form` | derivation `premises ⊢ conclusion`, or an equivalence `≡` |
| `statement` | one-line meaning, own words |
| `source` | section of the book the rule is distilled from |
| `executable` | `true` = applied by the audit engine (`internal/facts`); `false` = reference/retrieval only |

"Executable" rules are the deduction/rewrite rules the audit layer can apply to
claims and premises. Domain theorems (number theory, graphs, probability) are
catalogued for retrieval and agent reasoning, not run as audit checks.

## Executable fragment (P3, issue #329)

The audit engine (`internal/facts`) does not run the whole `executable` set.
It runs exactly the rules whose premise shape exists in the claim model:

| Rule id | Where it runs |
|---------|---------------|
| `logic.inference.modus-ponens` | `internal/facts` `Derive` / `CheckInference` |
| `logic.inference.modus-tollens` | `internal/facts` `Derive` / `CheckInference` |
| `proofs.contradiction.indirect`, Vinogradov set | `internal/facts/formal.go` |

`De Morgan` and the quantifier rules stay **reference only** for now: the claim
model has no connectives or quantifiers to rewrite, so wiring them would be
dead code. They become executable when a formula-bearing claim shape exists.
Everything else in the catalog is retrieval/vocabulary for the agent.

## Coverage

| File | Domain | Rules |
|------|--------|------:|
| `proofs.yml` | `proofs`, `number-theory` | 13 |
| `logic.yml` | `logic` | 27 |
| `induction.yml` | `induction` | 10 |

Parts II–V (number theory, graphs, counting, probability, recurrences) are a
later phase of epic #324.

## Proofs (§1–2)

| id | form | § |
|----|------|---|
| `proofs.axiomatic.method` | axioms + deductions ⊢ proposition | §1.3 |
| `proofs.proposition.terms` | theorem / lemma / corollary | §1.3 |
| `proofs.implication.assume` | assume P; derive Q ⊢ P → Q | §1.5.1 |
| `proofs.implication.contrapositive` | ¬Q → ¬P ⊢ P → Q | §1.5.2 |
| `proofs.iff.two-implications` | P → Q; Q → P ⊢ P ↔ Q | §1.6.1 |
| `proofs.iff.chain` | P ↔ R1 ↔ … ↔ Q ⊢ P ↔ Q | §1.6.2 |
| `proofs.cases.exhaustive` | P1∨…∨Pn; each ⇒ Q ⊢ Q | §1.7 |
| `proofs.contradiction.indirect` | ¬P → false ⊢ P | §1.8 |
| `wellordering.principle` | S ⊆ ℕ, S ≠ ∅ ⊢ min S exists | §2.1 |
| `wellordering.counterexample.template` | counterexample set → contradiction ⊢ ∀n P(n) | §2.2 |
| `wellordering.minimum.counterexample` | least counterexample; ∀k<c P(k) ⊢ P(c) | §2.2 |
| `number.factorization.into-primes` | n > 1 ⊢ n = p1·…·pk | §2.3 |
| `wellordering.well-ordered-set` | every nonempty subset has a least ⊢ well ordered | §2.4 |

## Logic (§3)

| id | form | § |
|----|------|---|
| `logic.converse.definition` | P → Q ⊢ converse Q → P (not equivalent) | §3.3.1 |
| `logic.valid.definition` | true under all assignments ⊢ valid | §3.3.2 |
| `logic.satisfiable.definition` | ∃ assignment true ⊢ satisfiable | §3.3.2 |
| `logic.equiv.validity` | valid(F ↔ G) ⊢ F ≡ G | §3.3.2 |
| `logic.valid.excluded-middle` | ⊢ P ∨ ¬P | §3.3.2 |
| `logic.valid.implication-transitive` | (P→Q) ∧ (Q→R) ⊢ P→R | §3.3.2 |
| `logic.satisfiable.dual` | satisfiable(F) ≡ ¬valid(¬F) | §3.3.2 |
| `logic.normal-form.disjunctive` | formula ⊢ OR of AND-terms | §3.4.1 |
| `logic.equiv.de-morgan` | ¬(P∧Q) ≡ ¬P∨¬Q ; ¬(P∨Q) ≡ ¬P∧¬Q | §3.4 |
| `logic.equiv.implication-material` | P → Q ≡ ¬P ∨ Q | §3.4 |
| `logic.equiv.contrapositive` | P → Q ≡ ¬Q → ¬P | §3.4 |
| `logic.equiv.double-negation` | ¬¬P ≡ P | §3.4 |
| `logic.equiv.idempotent` | P∧P ≡ P ; P∨P ≡ P | §3.4 |
| `logic.equiv.commutative` | P∧Q ≡ Q∧P ; P∨Q ≡ Q∨P | §3.4 |
| `logic.equiv.associative` | reassociation of ∧ and ∨ | §3.4 |
| `logic.equiv.distributive` | ∧ over ∨ and ∨ over ∧ | §3.4 |
| `logic.equiv.identity` | P∧T ≡ P ; P∨F ≡ P | §3.4 |
| `logic.equiv.annihilator` | P∨T ≡ T ; P∧F ≡ F | §3.4 |
| `logic.equiv.absorption` | P∨(P∧Q) ≡ P ; P∧(P∨Q) ≡ P | §3.4 |
| `logic.equiv.iff` | P ↔ Q ≡ (P→Q) ∧ (Q→P) | §3.4 |
| `logic.inference.modus-ponens` | P ; P → Q ⊢ Q | §3.4 |
| `logic.inference.modus-tollens` | ¬Q ; P → Q ⊢ ¬P | §3.4 |
| `logic.sat.problem` | formula ⊢ satisfiable? | §3.5 |
| `logic.predicate.definition` | P(x) predicate ⊢ P(a) proposition | §3.6 |
| `logic.quantifier.negation` | ¬∀x P ≡ ∃x ¬P ; ¬∃x P ≡ ∀x ¬P | §3.6 |
| `logic.quantifier.distribution` | ∀x(P→Q(x)) ≡ P→∀x Q(x) | §3.6 |
| `logic.quantifier.swap-universal` | ∀x∀y P ≡ ∀y∀x P | §3.6 |

## Induction (§5–6)

| id | form | § |
|----|------|---|
| `induction.ordinary.principle` | P(0); P(n)→P(n+1) ⊢ ∀n P(n) | §5.1 |
| `induction.ordinary.template` | base + step ⊢ ∀n P(n) | §5.1 |
| `induction.strong.principle` | ∀n((∀k<n P(k))→P(n)) ⊢ ∀n P(n) | §5.2 |
| `induction.equivalence.strong-weak-wellordering` | strong ≡ weak ≡ well ordering | §5.3 |
| `induction.invariant.state-machine` | preserved invariant ⊢ all reachable states | §5.4 |
| `induction.partial-correctness` | invariant ⇒ correct answer on halt | §5.4 |
| `induction.structural.principle` | base + constructors ⊢ all values | §6.1 |
| `induction.recursive.definition` | base + constructor clauses ⊢ set | §6.1 |
| `induction.recursive.delete` | n constructors ⊢ smaller sub-value exists | §6.1 |
| `induction.structural.matched-brackets` | base + wrap + concat ⊢ balanced | §6.2 |
