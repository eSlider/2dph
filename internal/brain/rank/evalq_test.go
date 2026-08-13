package rank

import "testing"

func TestEvalQuestionsAreThreeAndThreshold(t *testing.T) {
	if EvalRecallThreshold != 0.95 {
		t.Fatalf("threshold = %v", EvalRecallThreshold)
	}
	if len(EvalQuestions) != 3 {
		t.Fatalf("questions = %d, want 3", len(EvalQuestions))
	}
	for _, q := range EvalQuestions {
		if q.Query == "" || q.Fragment == "" {
			t.Fatalf("empty control: %+v", q)
		}
	}
}
