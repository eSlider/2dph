package ooimport

// Офлайн-тесты нормализации имени для UpdatePerson (фикс #271 acceptance):
// OnlyOffice JSON-PUT /crm/contact/person/{id} отвергает пустые строки
// firstName/lastName (400 "Value does not fall within the expected range"),
// пробел " " проходит и нормализуется сервером в пустоту при сохранении.
// Role-ящики с однословным именем (alle@, entwicklung@, info@) имеют пустой
// lastName — без нормализации линковка на компанию (RunCompanies) и
// дописывание about (RunBackfill) падают на них.

import "testing"

func TestNonEmptyName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", " "},
		{"   ", " "},
		{"\n\t ", " "},
		{"alle", "alle"},
		{"Wartung", "Wartung"},
		{"WhereGroup", "WhereGroup"},
		{"Astrid Emde", "Astrid Emde"},
	}
	for _, c := range cases {
		if got := nonEmptyName(c.in); got != c.want {
			t.Errorf("nonEmptyName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
