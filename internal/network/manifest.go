package network

// Manifest — CRM-манифест accept-связей сети: YAML-выход
// bin/network --accept-only (формат crmDoc/crmLink, L-9.5 #234; ADR-0012
// §сеть связей п.4). Общий контракт продюсера (bin/network) и коннектора в
// OnlyOffice CRM (bin/onlyoffice/import-network.go, N-1.2 #269).
type Manifest struct {
	Target string         `yaml:"target"`
	Links  []ManifestLink `yaml:"links"`
	Source string         `yaml:"source"`
}

// ManifestLink — одна accept-связь в CRM-манифесте. kind: person|company
// (сервис-аккаунты исключены продюсером, N-1.1 #268); premises — первые 5
// ссылок на Message/Commit, extraPremises — сколько ещё за пределами
// манифеста (полный список в --json).
type ManifestLink struct {
	Person   string            `yaml:"person"`
	Name     string            `yaml:"name,omitempty"`
	Kind     string            `yaml:"kind"`
	Msgs     int               `yaml:"msgs"`
	Threads  int               `yaml:"threads"`
	Replies  int               `yaml:"replies"`
	Period   string            `yaml:"period"`
	Projects []ManifestProject `yaml:"projects,omitempty"`
	Premises []Premise         `yaml:"premises"`
	Extra    int               `yaml:"extraPremises,omitempty"`
}

// ManifestProject — общий git-проект связи в манифесте.
type ManifestProject struct {
	Repo   string `yaml:"repo"`
	Period string `yaml:"period"`
}
