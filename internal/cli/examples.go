// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

// commandExamples are the --help examples of the commands the colleague
// guide (docs/guide.md) uses: "lead with examples" (clig.dev), as every gh
// command does. Commands only — the same in both languages. A command that
// sets its own Example keeps it.
var commandExamples = map[string]string{
	"evo configure": `  evo configure
  evo configure --profile test
  printf '%s' "$SECRET" | evo configure --key-id "$KEY_ID" --secret-stdin --project-id <UUID>`,
	"evo whoami": `  evo whoami
  evo --profile test whoami
  evo whoami --json`,
	"evo doctor": `  evo doctor
  evo doctor --json`,
	"evo init": `  evo init
  evo init --non-interactive
  evo init --image debian-12`,
	"evo status": `  evo status
  evo status --json`,
	"evo verify": `  evo verify
  evo deploy --yes && evo verify
  evo verify --json | jq -r '.[] | select(.healthy | not) | .port'`,
	"evo destroy": `  evo destroy --dry-run
  evo destroy
  evo destroy --yes`,
	"evo list-zones": `  evo list-zones
  evo list-zones --json`,
	"evo tools list": `  evo tools list
  evo tools list --json`,
	"evo tools describe": `  evo tools describe dns_zones
  evo tools describe provision --json`,
	"evo tools call": `  evo tools call dns_zones
  evo tools call vm_get --arg vm_id=<ID>
  evo tools call pg_operation --arg operation_id=<ID> --wait`,
	"evo mcp": `  evo mcp
  claude mcp add --transport stdio --scope project evo -- evo mcp
  MCP_BEARER_TOKEN="$(openssl rand -hex 32)" evo mcp --http :8080 --allow-project <UUID>`,
	"evo vm list": `  evo vm list
  evo vm list --json | jq -r '.vms[] | "\(.name)\t\(.state)"'
  evo --project-id <UUID> vm list`,
	"evo vm get": `  evo vm get my-vm
  evo vm get <ID> --json`,
	"evo vm create": `  evo vm create my-vm --dry-run
  evo vm create my-vm --image Ubuntu-24.04 --flavor lowcost10-2-4 --port 80 --port 443
  evo vm create astra-1 --image 'Astra Linux SE 1.8'
  evo vm create dash-1 --image OpensearchDashboards --meta opensearch_host=10.0.0.5 --meta opensearch_password=@./opensearch.pass
  pass show opensearch | evo vm create dash-2 --image OpensearchDashboards --meta opensearch_host=10.0.0.5 --meta opensearch_password=- --yes
  evo vm create worker-1 --no-public-ip --yes`,
	"evo vm delete": `  evo vm delete my-vm --dry-run
  evo vm delete my-vm`,
	"evo image list": `  evo image list astra
  evo image list --type public
  evo image list ubuntu --zone ru.AZ-2 --json | jq -r '.images[] | "\(.id)\t\(.name)"'`,
	"evo image get": `  evo image get AstraLinuxSmolensk1.8.3KES
  evo image get <UUID> --json`,
	"evo disk list": `  evo disk list
  evo disk list --json`,
	"evo k8s list": `  evo k8s list`,
	"evo k8s kubeconfig": `  evo k8s kubeconfig <cluster-id> > kubeconfig.yaml
  evo k8s kubeconfig <cluster-id> --embed-token=false > kubeconfig.yaml`,
	"evo k8s addons": `  evo k8s addons cert
  evo k8s addons --category Сети
  evo k8s addons --json | jq -r '.addons[].name'`,
	"evo k8s addon-versions": `  evo k8s addon-versions cert-manager
  evo k8s addon-versions cert-manager --full --json | jq '.releases[0].configuration_schema'`,
	"evo k8s add-addon": `  evo k8s add-addon <cluster-id> cert-manager --dry-run
  evo k8s add-addon <cluster-id> cert-manager --config settings.json`,
	"evo pg list": `  evo pg list`,
	"evo pg create": `  evo pg create app-db --dry-run
  evo pg create app-db --cpu 2 --ram 4`,
	"evo s3 buckets": `  evo s3 buckets`,
	"evo s3 ls": `  evo s3 ls my-bucket
  evo s3 ls my-bucket --prefix backups/`,
}

// applyExamples sets commandExamples on the tree under root.
func applyExamples(root *cobra.Command) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if ex, ok := commandExamples[c.CommandPath()]; ok && c.Example == "" {
			c.Example = ex
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}
