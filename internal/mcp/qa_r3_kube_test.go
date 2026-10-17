package mcp

import (
	"strings"
	"testing"
)

// M1–M4 (docs/internal/qa-r3.md): политика k8s_kubectl по HTTP пропускала
// аргументы, которые печатают данные Secret или читают файлы хоста без
// --allow-secret-export. Теперь каждый случай отклоняется.
func TestQAr3_KubectlHTTPCheckSecretBypasses(t *testing.T) {
	cases := []string{
		// M1: шаблоны и любые форматы вывода, кроме name/wide, для Secret.
		"get secret x --template={{.data}}",
		"get secret x --template {{.data}}",
		"get secrets --go-template={{.items}}",
		"get secret x -o go-template={{.data}}",
		"get secret x -ogo-template={{.data}}",
		"get secret x -o=jsonpath={.data.password}",
		"get secret x -ojsonpath-as-json={.data}",
		"get secret x -o custom-columns=P:.data.password",
		"get secret x -oyaml",
		"get secret x -o=yaml",
		"get secret x --output yaml",
		"get secret x --output=json",
		"get secret x -Aoyaml",
		"get secret x -Ao yaml",
		"get secret x -o name -o yaml",
		"get secret,configmap -o yaml",
		"get configmap,secrets -o json",
		"get secret/x -o yaml",
		"get secrets/x -o yaml",
		"get Secret.v1 x -o yaml",
		"get -o yaml -- secret x",
		"annotate secret x --list",
		"apply view-last-applied secret/x",
		"set env deploy/x --list --resolve",
		// M2: журнал client-go при -v>=8 печатает тела ответов API.
		"get secret x -o name -v=8",
		"describe secret x -v=9",
		"get pods -v 8",
		"get pods -v8",
		"get pods --v=8",
		"get pods --v 8",
		"get pods -Av8",
		"get pods --vmodule=*=8",
		// M3: та же проверка вывода для всех verbs, не только get.
		"label secret x a=b -o yaml --dry-run=server",
		"annotate secret x a=b -oyaml --dry-run=server",
		"patch secret x -p {} -o json --dry-run=server",
		"set data secret/x -o yaml",
		"apply secret x -o yaml",
		"edit secret x -o yaml",
		"create secret generic s --from-literal=a=b -o yaml --dry-run=server",
		"wait secret/x --for=create -o yaml",
		// M4: флаги, читающие файлы хоста.
		"create secret tls t --cert=/etc/ssl/host.pem --key=/etc/ssl/host.key --dry-run=client -o yaml",
		"create secret tls t --cert /etc/ssl/host.pem --key /etc/ssl/host.key",
		"create secret generic s --ca-cert=/etc/ssl/ca.pem",
		"create secret generic s --from-file=/etc/passwd",
		"create secret generic s --from-file /etc/passwd",
		"create secret generic s --from-env-file=/etc/environment",
		"create secret docker-registry s --from-file=.dockerconfigjson=/root/.docker/config.json",
		"create configmap c --from_file=/etc/passwd",
		"get pods --client_key=/etc/x",
		"debug pod/x --custom=/etc/x.json --image=busybox",
		"logs pod/x -pf",
		// Релизный аудит, R-05: --kuberc читает файл хоста (оракул путей)
		// и применяет его алиасы и значения флагов.
		"get pods --kuberc=/etc/passwd",
		"get pods --kuberc /srv/shared/kuberc",
	}
	for _, c := range cases {
		if err := kubectlHTTPCheck(strings.Fields(c), false); err == nil {
			t.Errorf("%q: allowed over HTTP, must be refused", c)
		}
	}

	// То, что данных Secret не печатает, работает как раньше.
	allowed := []string{
		"get pods",
		"get pods -A -o yaml",
		"get pods -Ao wide",
		"get pods -it",
		"get pods -n kube-system -oyaml",
		"get pods -nkube-system",
		"get secrets",
		"get secret x",
		"get secret -o name",
		"get secret x -oname",
		"get secret x -o=name",
		"get secrets -o wide",
		"get secrets -A --show-labels",
		"describe secret x",
		"describe secrets",
		"label secret x a=b",
		"create secret generic s --from-literal=a=b",
		"create secret generic s --from-literal=a=b --dry-run=client -o name",
		"get configmap x --template={{.data}}",
		"get configmap x -o yaml",
		"annotate pod x --list",
		"set env deploy/x --list",
		"logs pod/x -p",
		"exec pod-0 -- kubectl get secret x -o yaml -v=9",
		"run t --image=busybox --restart=Never -- sh -c echo",
	}
	for _, c := range allowed {
		if err := kubectlHTTPCheck(strings.Fields(c), false); err != nil {
			t.Errorf("%q refused over HTTP: %v", c, err)
		}
	}

	// --allow-secret-export открывает данные Secret, но не файлы хоста
	// и не подробный журнал.
	for _, c := range []string{
		"get secret x --template={{.data}}",
		"label secret x a=b -o yaml --dry-run=server",
		"annotate secret x --list",
		"set env deploy/x --list --resolve",
	} {
		if err := kubectlHTTPCheck(strings.Fields(c), true); err != nil {
			t.Errorf("%q with --allow-secret-export refused: %v", c, err)
		}
	}
	for _, c := range []string{
		"create secret tls t --cert=/etc/ssl/host.pem --key=/etc/ssl/host.key",
		"get secret x -o name -v=8",
		"get pods --vmodule=*=8",
	} {
		if err := kubectlHTTPCheck(strings.Fields(c), true); err == nil {
			t.Errorf("%q: --allow-secret-export opened it", c)
		}
	}
}
