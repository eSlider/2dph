package rules

import (
	"regexp"
	"strconv"
	"strings"
)

// Chapter — глава источника, выделенная из layout-текста (pdftotext -layout).
// Start/End — границы в строках исходного текста (1-based, End исключающий);
// Body — текст главы без изменения (нужен для извлечения правил, не для
// коммита: лицензированный текст не попадает в репозиторий, epic #324).
type Chapter struct {
	Number int
	Title  string
	Start  int
	End    int
	Body   string
}

var (
	// pageMarkerRe — колонтитул страницы в выводе pdftotext:
	// «mcs» — 2015/5/18 — 1:43 — page 41 — #73.
	pageMarkerRe = regexp.MustCompile(`—\s*page\s+\d+\s*—\s*#\d+`)
	// chapterRe — строка заголовка главы: «3   Logical Formulas».
	chapterRe = regexp.MustCompile(`^[ \t]*(\d{1,2})[ \t]{2,}(\S(?:.*\S)?)[ \t]*$`)
)

// SplitChapters режет layout-текст на главы 1..N. Заголовок главы — первая
// непустая строка страницы (сразу после колонтитула), совпадающая с
// chapterRe; номер обязан идти строго по порядку (1,2,3,…), а «Chapter N» и
// «page N» в колонтитулах отсекаются. Такой контракт устойчив к бегущим
// колонтитулам («18   Chapter 3   Logical Formulas») и к оглавлению.
func SplitChapters(text string) []Chapter {
	lines := strings.Split(text, "\n")
	var starts []int
	var titles []string
	expected := 1

	for i := 0; i < len(lines); i++ {
		if !pageMarkerRe.MatchString(lines[i]) {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j >= len(lines) {
			break
		}
		m := chapterRe.FindStringSubmatch(lines[j])
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n != expected {
			continue
		}
		title := strings.TrimSpace(m[2])
		if title == "" || strings.Contains(title, "Chapter") {
			continue
		}
		starts = append(starts, j)
		titles = append(titles, title)
		expected++
	}

	chapters := make([]Chapter, 0, len(starts))
	for k, s := range starts {
		end := len(lines)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		chapters = append(chapters, Chapter{
			Number: k + 1,
			Title:  titles[k],
			Start:  s + 1,
			End:    end,
			Body:   strings.Join(lines[s:end], "\n"),
		})
	}
	return chapters
}
