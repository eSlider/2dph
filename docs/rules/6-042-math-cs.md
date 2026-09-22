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
| `sets.yml` | `sets` | 13 |
| `infinite-sets.yml` | `infinite-sets` | 10 |
| `number-theory.yml` | `number-theory` | 19 |
| `graphs.yml` | `graphs` | 28 |
| `counting.yml` | `counting` | 27 |
| `probability.yml` | `probability` | 31 |
| `recurrences.yml` | `recurrences` | 11 |

P4 (issue #331) completed the catalog with Parts II–V. Every rule in the new
domains is `executable: false`: they are retrieval/vocabulary for the agent, not
audit checks. Only `proofs`, `logic` and `induction` carry executable rules.

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

## Sets (§4)

| id | form | § |
|----|------|---|
| `sets.subset.definition` | x ∈ S → x ∈ T  ⊢  S ⊆ T | §4.1.2 |
| `sets.combine.operations` | A ∪ B = {x : x ∈ A ∨ x ∈ B} ; A ∩ B = {x : x ∈ A ∧ x ∈ B} ; A \ B = {x : x ∈ A ∧ x ∉ B} | §4.1.2 |
| `sets.complement.definition` | A̅ = D \ A ; A ∪ A̅ = D ; A ∩ A̅ = ∅ | §4.1.2 |
| `sets.power-set.definition` | B ∈ pow(A)  iff  B ⊆ A | §4.1.3 |
| `sets.builder.notation` | {x ∈ S : P(x)}  ⊢  the subset of S whose elements satisfy P | §4.1.4 |
| `sets.equality.extensionality` | ∀z (z ∈ X ↔ z ∈ Y)  ⊢  X = Y | §4.1.5 |
| `sets.sequence.definition` | (a1, …, an) ordered, terms may repeat ; S1 × … × Sn = all compatible tuples | §4.2 |
| `sets.function.definition` | f : A → B ; f(S) = {f(s) : s ∈ S} ; range(f) = f(domain(f)) | §4.3.1 |
| `sets.function.composition` | (g ∘ f)(x) = g(f(x))  with  f : A → B and g : B → C | §4.3.2 |
| `sets.relation.binary` | R : A → B ; graph(R) ⊆ A × B ; a R b means (a, b) ∈ graph(R) | §4.4 |
| `sets.relation.images` | R(Y) = {b ∈ B : a R b for some a ∈ Y} ; b R⁻¹ a  iff  a R b | §4.4.2 |
| `sets.cardinality.mapping-rules` | \|A\| ≥ \|B\| iff A surj B ; \|A\| ≤ \|B\| iff A inj B ; \|A\| = \|B\| iff A bij B | §4.5 |
| `sets.cardinality.subsets` | \|A\| = n  ⊢  \|pow(A)\| = 2^n | §4.5.1 |

## Infinite sets (§7)

| id | form | § |
|----|------|---|
| `infinite.strict.definition` | A strict B  iff  NOT (A surj B) | §7.1 |
| `infinite.schroder-bernstein` | A surj B ; B surj A  ⊢  A bij B | §7.1 |
| `infinite.countable.definition` | C countable iff C finite or ℕ bij C ; equivalently ℕ surj C | §7.1.2 |
| `infinite.add-element.bijection` | b ∉ A  ⊢  A infinite iff A bij A ∪ {b} | §7.1.1 |
| `infinite.cantor.power-set` | A strict pow(A) | §7.1.3 |
| `infinite.diagonal.argument` | any listing of infinite bit strings  ⊢  a string omitted by flipping the diagonal | §7.1.4 |
| `infinite.halting.undecidable` | decider for {s : program P_s halts on input s}  ⊢  contradiction by diagonalization | §7.2 |
| `infinite.russell.paradox` | W = {S : S ∉ S}  ⊢  W ∈ W iff W ∉ W | §7.3.1 |
| `infinite.zfc.axioms` | extensionality, pairing, union, infinity, subset, power set, replacement, foundation, choice  ⊢  standard mathematics | §7.3.2 |
| `infinite.foundation.no-self-member` | Foundation  ⊢  S ∉ S for every set S | §7.3.3 |

## Number theory (§8)

| id | form | § |
|----|------|---|
| `number.divisibility.definition` | a \| b  iff  ∃k ∈ ℤ (a·k = b) | §8.1 |
| `number.divisibility.properties` | a \| b ; b \| c  ⊢  a \| c  ;  a \| b ; a \| c  ⊢  a \| (sb + tc) | §8.1.1 |
| `number.division-theorem` | d > 0  ⊢  n = qd + r for unique q, r with 0 ≤ r < d | §8.1.2 |
| `number.gcd.definition` | gcd(a, b) = the greatest integer dividing both a and b | §8.2 |
| `number.gcd.euclid` | b ≠ 0  ⊢  gcd(a, b) = gcd(b, rem(a, b)) | §8.2.1 |
| `number.gcd.linear-combination` | ⊢  gcd(a, b) = sa + tb for some integers s and t | §8.2.2 |
| `number.prime.definition` | p prime iff p > 1 and the only positive divisors of p are 1 and p | §8.3 |
| `number.prime.divides-product` | p prime ; p \| ab  ⊢  p \| a or p \| b | §8.4.1 |
| `number.arithmetic.fundamental-unique` | every positive integer  ⊢  a unique weakly decreasing product of primes | §8.4 |
| `number.prime.number-theorem` | π(n) = number of primes ≤ n ; lim π(n) / (n / ln n) = 1 | §8.3 |
| `number.congruence.definition` | a ≡ b (mod n)  iff  n \| (a − b)  iff  rem(a, n) = rem(b, n) | §8.6 |
| `number.congruence.arithmetic` | a ≡ b ; c ≡ d (mod n)  ⊢  a + c ≡ b + d ; a·c ≡ b·d (mod n) | §8.6 |
| `number.remainder-arithmetic` | operands of + and · may be replaced by their remainders modulo n | §8.7 |
| `number.inverse.relative-prime` | gcd(k, n) = 1  iff  k has an inverse in ℤ_n  iff  k is cancellable in ℤ_n | §8.9 |
| `number.cancellation.rule` | k invertible in ℤ_n  ⊢  ka = kb implies a = b | §8.9.2 |
| `number.euler.theorem` | gcd(k, n) = 1  ⊢  k^φ(n) ≡ 1 (mod n) | §8.10 |
| `number.totient.definition` | φ(n) = \|{k ∈ [0, n) : gcd(k, n) = 1}\| ; φ(p) = p − 1 ; φ(pq) = (p − 1)(q − 1) | §8.10.1 |
| `number.fermat.little` | p prime ; p ∤ k  ⊢  k^(p − 1) ≡ 1 (mod p) | §8.10 |
| `number.rsa.cryptosystem` | n = pq ; gcd(e, (p − 1)(q − 1)) = 1 ; d = e⁻¹ mod (p − 1)(q − 1)  ⊢  m = (m^e)^d (mod n) | §8.11 |

## Graphs (§9–12)

| id | form | § |
|----|------|---|
| `graphs.digraph.definition` | digraph = vertices V + directed edges ⟨u → v⟩ ⊆ V × V | §9 |
| `graphs.degree.sum` | Σ_v indeg(v) = Σ_v outdeg(v) = \|E\| | §9.1 |
| `graphs.walk.path.definition` | walk = alternating vertices and edges ; path = walk with distinct vertices ; cycle = positive closed walk, distinct except ends | §9.2 |
| `graphs.walk-relation` | G⁺ is transitive ; G* is reflexive and transitive ; a walk  ⊢  a path no longer than \|V\| | §9.4 |
| `graphs.dag.definition` | a digraph with no cycles  ⊢  DAG | §9.5 |
| `graphs.topological-sort` | finite DAG  ⊢  a list with every vertex before all vertices reachable from it | §9.5.1 |
| `graphs.dilworth.lemma` | DAG with n vertices  ⊢  a chain larger than t or an antichain of size at least n / t | §9.5.3 |
| `graphs.partial-order.strict` | transitive and irreflexive  ⊢  strict partial order  iff  positive walk relation of a DAG | §9.6.2 |
| `graphs.partial-order.weak` | transitive, reflexive and antisymmetric  ⊢  weak partial order  iff  walk relation of a DAG | §9.6.3 |
| `graphs.partial-order.set-containment` | weak order  ⊢  isomorphic to ⊆ on a family of sets ; strict order  ⊢  isomorphic to ⊂ | §9.7 |
| `graphs.equivalence-relation` | reflexive, symmetric and transitive  ⊢  equivalence relation ; its classes partition the set | §9.10 |
| `graphs.simple.definition` | undirected edges ⟨u — v⟩ ; no loops and no repeated edges  ⊢  simple graph | §11.1 |
| `graphs.simple.handshaking` | Σ_v deg(v) = 2 \|E\| | §11.2.1 |
| `graphs.isomorphism.definition` | edge-preserving bijection f : V(G) → V(H)  ⊢  G ≅ H | §11.4 |
| `graphs.bipartite.definition` | V = L ∪ R, L ∩ R = ∅, every edge joins L to R  ⊢  bipartite | §11.5 |
| `graphs.hall.matching` | bipartite G  ⊢  a matching covering L(G) exists iff no subset of L(G) is a bottleneck | §11.5.1 |
| `graphs.stable-marriage` | preference lists on both sides  ⊢  a stable matching exists ; the mating ritual finds one | §11.6 |
| `graphs.coloring.definition` | adjacent vertices get different colors ; χ(G) = fewest colors needed | §11.7 |
| `graphs.coloring.degree-bound` | maximum degree at most k  ⊢  (k + 1)-colorable | §11.7.2 |
| `graphs.odd-cycle.2-colorability` | contains an odd cycle  ≡  not 2-colorable  ≡  contains an odd closed walk | §11.9.2 |
| `graphs.connectivity.components` | G has at least \|V(G)\| − \|E(G)\| connected components | §11.9 |
| `graphs.tree.properties` | tree  ⊢  unique path between vertices ; \|V\| = \|E\| + 1 ; every edge is a cut edge ; at least two leaves | §11.10.2 |
| `graphs.spanning-tree` | connected graph  ⊢  a spanning tree exists | §11.10.3 |
| `graphs.planar.definition` | drawing in the plane with no edge crossings  ⊢  planar ; the regions are faces | §12.2 |
| `graphs.euler.formula` | connected planar embedding with v vertices, e edges and f faces  ⊢  v − e + f = 2 | §12.3 |
| `graphs.planar.edge-bound` | planar with v ≥ 3  ⊢  e ≤ 3v − 6 ; bipartite planar  ⊢  e ≤ 2v − 4 | §12.4 |
| `graphs.planar.k5-k33` | K5 has 10 > 3·5 − 6 edges ; K3,3 has 9 > 2·6 − 4  ⊢  not planar | §12.5 |
| `graphs.planar.five-color` | G planar  ⊢  χ(G) ≤ 5 | §12.6 |

## Counting (§13–15)

| id | form | § |
|----|------|---|
| `counting.geometric-sum` | Σ_{i=0}^n x^i = (1 − x^{n+1}) / (1 − x) ; Σ_{i=0}^∞ x^i = 1 / (1 − x) for \|x\| < 1 | §13.1 |
| `counting.perturbation-method` | S = Σ x^i ; xS = Σ x^{i+1} ; S − xS  ⊢  closed form | §13.1.2 |
| `counting.sums-of-powers` | Σ_{i=1}^n i = n(n + 1)/2 ; Σ_{i=1}^n i² = n(n + 1)(2n + 1)/6 | §13.2 |
| `counting.integral-bounds` | f weakly increasing: I + f(1) ≤ Σ_{i=1}^n f(i) ≤ I + f(n), where I = ∫_1^n f | §13.3 |
| `counting.harmonic.number` | H_n = 1 + 1/2 + … + 1/n ; H_n ~ ln n | §13.4.2 |
| `counting.asymptotic.equality` | f ~ g  iff  lim f(x) / g(x) = 1 | §13.4.3 |
| `counting.stirling.formula` | n! ~ √(2πn) · (n / e)^n | §13.5.1 |
| `counting.asymptotic.little-o` | f = o(g)  iff  lim f(x) / g(x) = 0 | §13.7.1 |
| `counting.asymptotic.big-o` | f = O(g)  iff  ∃c, x0 ∀x ≥ x0 (\|f(x)\| ≤ c·g(x)) | §13.7.2 |
| `counting.asymptotic.theta` | f = Θ(g)  iff  f = O(g) and g = O(f) | §13.7.3 |
| `counting.bijection-rule` | bijection f : A → B  ⊢  \|A\| = \|B\| | §14.1.1 |
| `counting.product-rule` | \|P1 × … × Pn\| = \|P1\| · \|P2\| · … · \|Pn\| | §14.2.1 |
| `counting.sum-rule` | A_i pairwise disjoint  ⊢  \|A1 ∪ … ∪ An\| = \|A1\| + … + \|An\| | §14.2.3 |
| `counting.generalized-product` | n1 choices, then n2 given the first, …  ⊢  \|S\| = n1 · n2 · … · nk | §14.3 |
| `counting.division-rule` | f : A → B is k-to-1  ⊢  \|A\| = k · \|B\| | §14.4 |
| `counting.subset-rule` | C(n, k) = n! / (k! · (n − k)!) | §14.5.1 |
| `counting.bookkeeper-rule` | sequences with k_i copies of letter l_i  ⊢  (Σ k_i)! / Π k_i! | §14.6.2 |
| `counting.binomial-theorem` | (a + b)^n = Σ_{k=0}^n C(n, k) · a^{n−k} · b^k | §14.6.3 |
| `counting.pigeonhole` | \|A\| > \|B\| ; f : A → B total  ⊢  two different elements share an image | §14.8 |
| `counting.inclusion-exclusion` | \|∪ S_i\| = Σ\|S_i\| − Σ\|S_i ∩ S_j\| + Σ\|S_i ∩ S_j ∩ S_k\| − … | §14.9 |
| `counting.combinatorial-proof` | count the same set two ways  ⊢  algebraic identity | §14.10 |
| `counting.pascal.identity` | C(n, k) = C(n − 1, k − 1) + C(n − 1, k) | §14.10.1 |
| `counting.generating-function.definition` | F(x) = Σ_{n≥0} a_n x^n represents the sequence (a_n) | §15.1 |
| `counting.generating-function.convolution` | A(x)·B(x)  ⊢  the coefficient of x^n is Σ_i a_i · b_{n−i} | §15.2.3 |
| `counting.partial-fractions` | p(x) / Π(1 − α_i x) = Σ c_i / (1 − α_i x)  ⊢  coefficients from geometric series | §15.3 |
| `counting.linear-recurrence.generating-function` | recurrence  ⊢  rational generating function  ⊢  closed form for f(n) | §15.4 |
| `counting.formal-power-series` | sequences with convolution product  ⊢  commutative ring of formal power series | §15.5 |

## Probability (§16–20)

| id | form | § |
|----|------|---|
| `probability.sample-space.definition` | countable nonempty S ; outcomes ω ∈ S ; any subset of S is an event | §16.5.1 |
| `probability.function.definition` | Pr : S → ℝ ; Pr[ω] ≥ 0 ; Σ_{ω} Pr[ω] = 1 ; Pr[E] = Σ_{ω∈E} Pr[ω] | §16.5.1 |
| `probability.four-step-method` | find the sample space ; define events ; assign outcome probabilities ; sum outcomes in the event | §16.2 |
| `probability.tree.edge-product` | Pr[outcome] = product of the edge probabilities on the root-to-leaf path | §16.2.3 |
| `probability.uniform.definition` | Pr[ω] equal for all ω  ⊢  Pr[E] = \|E\| / \|S\| | §16.5.3 |
| `probability.sum-rule` | E_i pairwise disjoint  ⊢  Pr[∪ E_i] = Σ Pr[E_i] | §16.5.2 |
| `probability.complement.rule` | Pr[A̅] = 1 − Pr[A] | §16.5.2 |
| `probability.inclusion-exclusion` | Pr[A ∪ B] = Pr[A] + Pr[B] − Pr[A ∩ B] | §16.5.2 |
| `probability.union-bound` | Pr[E1 ∪ … ∪ En] ≤ Pr[E1] + … + Pr[En] | §16.5.2 |
| `probability.conditional.definition` | Pr[A \| B] = Pr[A ∩ B] / Pr[B]  for Pr[B] > 0 | §17.2 |
| `probability.product-rule` | Pr[E1 ∩ … ∩ En] = Pr[E1] · Pr[E2 \| E1] · … · Pr[En \| E1 ∩ … ∩ E_{n−1}] | §17.4 |
| `probability.total-probability` | E_i disjoint and covering  ⊢  Pr[A] = Σ Pr[A \| E_i] · Pr[E_i] | §17.5 |
| `probability.bayes.rule` | Pr[E1 \| A] = Pr[A \| E1] · Pr[E1] / Σ_i Pr[A \| E_i] · Pr[E_i] | §17.5 |
| `probability.independence.definition` | Pr[A \| B] = Pr[A]  iff  Pr[A ∩ B] = Pr[A] · Pr[B] | §17.7 |
| `probability.mutual-independence` | every subset of the events satisfies Pr[∩] = Π Pr[·]  ⊢  mutually independent | §17.8 |
| `probability.random-variable.definition` | R : S → V ; event [R = x] = {ω : R(ω) = x} ; I_E is 1 on E and 0 elsewhere | §18.1 |
| `probability.distribution.pdf-cdf` | PDF_R(x) = Pr[R = x] ; CDF_R(x) = Pr[R ≤ x] = Σ_{y≤x} PDF_R(y) | §18.3 |
| `probability.distribution.common` | Bernoulli: f_p(0) = 1 − p, f_p(1) = p ; uniform: f(v) = 1/n ; binomial: C(n, k) p^k (1 − p)^{n−k} | §18.3 |
| `probability.expectation.definition` | Ex[R] = Σ_ω R(ω) · Pr[ω] = Σ_x x · Pr[R = x] | §18.4 |
| `probability.indicator.expectation` | Ex[I_A] = Pr[A] | §18.4.3 |
| `probability.expectation.linearity` | Ex[Σ a_i R_i] = Σ a_i Ex[R_i] | §18.5 |
| `probability.conditional-expectation` | Ex[R \| A] = Σ_ω R(ω) · Pr[ω \| A] | §18.4.5 |
| `probability.markov` | R ≥ 0 and x > 0  ⊢  Pr[R ≥ x] ≤ Ex[R] / x | §19.1 |
| `probability.variance.definition` | Var[R] = Ex[(R − Ex[R])²] ; σ_R = √Var[R] | §19.2.2 |
| `probability.chebyshev` | Pr[\|R − Ex[R]\| ≥ x] ≤ Var[R] / x² | §19.2 |
| `probability.variance.formula` | Var[R] = Ex[R²] − (Ex[R])² | §19.3.1 |
| `probability.variance.sum` | R and S independent  ⊢  Var[R + S] = Var[R] + Var[S] | §19.3.4 |
| `probability.chernoff` | T = Σ T_i with 0 ≤ T_i ≤ 1 independent and c ≥ 1  ⊢  Pr[T ≥ c Ex[T]] ≤ e^{−β(c) Ex[T]}, β(c) = c ln c − c + 1 | §19.6.2 |
| `probability.murphy.law` | T = Σ T_i a sum of independent indicators  ⊢  Pr[T = 0] ≤ e^{−Ex[T]} | §19.6.8 |
| `probability.gamblers-ruin` | start n, target T, win probability p, r = (1 − p)/p  ⊢  Pr[win] = n/T if p = 1/2, else (r^n − 1)/(r^T − 1) | §20.1 |
| `probability.random-walk.stationary` | π(x) = Σ_{y→x} π(y) / outdeg(y) ; Σ_x π(x) = 1  ⊢  stationary distribution | §20.2.3 |

## Recurrences (§21)

| id | form | § |
|----|------|---|
| `recurrences.guess-verify` | compute early terms ; guess a closed form ; prove it by induction  ⊢  solution | §21.1 |
| `recurrences.plug-chug` | expand T(n) into earlier terms until a pattern appears ; substitute a known base value  ⊢  closed form | §21.1.2 |
| `recurrences.hanoi.solution` | t_0 = 0 ; t_n = 2 t_{n−1} + 1  ⊢  t_n = 2^n − 1 | §21.1 |
| `recurrences.hanoi.lower-bound` | the largest disk must move twice, so t_n ≥ 2 t_{n−1} + 1  ⊢  t_n = 2^n − 1 | §21.1.1 |
| `recurrences.merge-sort.recurrence` | T_1 = 0 ; T_n = 2 T_{n/2} + n − 1  ⊢  T_n = n log n − n + 1 | §21.2.1 |
| `recurrences.linear.homogeneous` | f(n) = a1 f(n−1) + … + ad f(n−d) ; characteristic x^d = a1 x^{d−1} + … + ad  ⊢  solutions r^n, n r^n, … | §21.3.2 |
| `recurrences.linear.general` | general solution = homogeneous solution + particular solution ; fit boundary conditions  ⊢  closed form | §21.3.3 |
| `recurrences.linear.particular-guess` | guess a particular solution with the form of the inhomogeneous term (polynomial or exponential) | §21.3.4 |
| `recurrences.divide-conquer.definition` | T(n) = Σ_i a_i T(b_i n) + g(n)  with  0 < b_i < 1 | §21.4 |
| `recurrences.akra-bazzi` | Σ a_i b_i^p = 1  ⊢  T(n) = Θ(n^p (1 + ∫_1^n g(u)/u^{p+1} du)) | §21.4.3 |
| `recurrences.master-theorem` | T(n) = a T(n/b) + g(n) ; compare g(n) with n^{log_b a}  ⊢  matching asymptotic case | §21.4.4 |
