package kubetools

import (
	"slices"
	"testing"
)

func TestChildEnvDropsServerSecrets(t *testing.T) {
	got := childEnv([]string{
		"PATH=/usr/bin",
		"CLOUDRU_KEY_ID=k",
		"CLOUDRU_SECRET=s",
		"CLOUDRU_S3_SECRET_KEY=s3",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_ID=ek",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET=es",
		"EVOLUTION_PROJECT_ID=p",
		"MCP_BEARER_TOKEN=t",
		"HOME=/home/u",
		"HTTPS_PROXY=http://proxy:3128",
		"KUBERC=/home/u/.kube/kuberc",
	})
	// KUBERC=off: ~/.kube/kuberc (алиасы, значения флагов по умолчанию) не
	// меняет команды kubectl под evo; своё значение KUBERC не доходит
	// (релизный аудит, R-05).
	want := []string{"PATH=/usr/bin", "EVOLUTION_PROJECT_ID=p", "HOME=/home/u", "HTTPS_PROXY=http://proxy:3128", "KUBERC=off"}
	if !slices.Equal(got, want) {
		t.Fatalf("childEnv = %v, want %v", got, want)
	}
}
