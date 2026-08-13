package rank

// BM25 ranks best-first, so the top hits are the *highest* scores; cosine
// distance ranks best-first ascending. Both mirror kblib.py.
const FTSStmt = "CALL QUERY_FTS_INDEX('Leaf', 'id', $q) " +
	"RETURN node.id, node.text, node.root, node.source, score ORDER BY score DESC LIMIT $n"

const VecStmt = "CALL QUERY_VECTOR_INDEX('Leaf', 'Leaf_vec', $q, $n) " +
	"RETURN node.id, node.text, node.root, node.source, distance ORDER BY distance LIMIT $n"
