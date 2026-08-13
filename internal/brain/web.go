package brain

import (
	"context"

	"github.com/eSlider/2dph/internal/brain/rank"
	"github.com/eSlider/2dph/internal/websearch"
)

func lookupWeb(ctx context.Context, query string) rank.SecondSource {
	o := websearch.Lookup(ctx, query, websearch.LookupOpt{Limit: 5})
	return toSecond(o)
}

func toSecond(o websearch.Output) rank.SecondSource {
	hits := make([]rank.SecondSourceHit, 0, len(o.Results))
	for _, h := range o.Results {
		hits = append(hits, rank.SecondSourceHit{
			Rank:    h.Rank,
			Title:   h.Title,
			URL:     h.URL,
			Snippet: h.Snippet,
			Engine:  h.Engine,
		})
	}
	return rank.SecondSource{
		Status:  o.Status,
		Note:    o.Note,
		Cached:  o.Cached,
		Results: hits,
	}
}

func secondToDict(w rank.SecondSource) Dict {
	d := Dict{
		{"status", w.Status},
	}
	if w.Note != "" {
		d = append(d, KV{"note", w.Note})
	}
	if w.Cached {
		d = append(d, KV{"cached", true})
	}
	rows := make([]any, 0, len(w.Results))
	for _, h := range w.Results {
		rows = append(rows, Dict{
			{"rank", h.Rank},
			{"title", h.Title},
			{"url", h.URL},
			{"snippet", h.Snippet},
			{"engine", h.Engine},
		})
	}
	d = append(d, KV{"results", rows})
	return d
}
