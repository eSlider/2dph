package rank

// BM25 ranks best-first, so the top hits are the *highest* scores; cosine
// distance ranks best-first ascending. Both mirror kblib.py.
const FTSStmt = "CALL QUERY_FTS_INDEX('Leaf', 'id', $q) " +
	"RETURN node.id, node.text, node.root, node.source, score, node.confidence, " +
	"node.valid_from, node.valid_to ORDER BY score DESC LIMIT $n"

const VecStmt = "CALL QUERY_VECTOR_INDEX('Leaf', 'Leaf_vec', $q, $n) " +
	"RETURN node.id, node.text, node.root, node.source, distance, node.confidence, " +
	"node.valid_from, node.valid_to ORDER BY distance LIMIT $n"

// HopStmt is the Cypher walk from a search hit. Depth 1 = File, 2 = Commit, 3 = Person.
func HopStmt(depth int) string {
	switch depth {
	case 1:
		return "MATCH (l:Leaf {id:$id})-[:FROM_FILE]->(f:File) RETURN f.id, f.path, 1"
	case 2:
		return "MATCH (l:Leaf {id:$id})-[:FROM_FILE]->(f:File)-[:HAS_VERSION]->(c:Commit) RETURN c.id, c.subject, 2"
	case 3:
		return "MATCH (l:Leaf {id:$id})-[:FROM_FILE]->(f:File)-[:HAS_VERSION]->(c:Commit)-[:AUTHORED]->(p:Person) RETURN p.id, p.name, 3"
	default:
		return ""
	}
}

func HopLabel(depth int) string {
	switch depth {
	case 1:
		return "File"
	case 2:
		return "Commit"
	case 3:
		return "Person"
	default:
		return ""
	}
}
