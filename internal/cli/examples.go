// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

// commandExamples are the --help examples of the commands the colleague
// guide (docs/guide.md) uses, and of every command the CLI ↔ MCP parity
// plan added (spec.md REQ-3; TestCommandExamples_NewCommands): "lead with
// examples" (clig.dev), as every gh command does. Commands only — the same
// in both languages. A command that sets its own Example keeps it.
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

	// dns
	"evo dns zones": `  evo dns zones
  evo dns zones --json | jq -r '.zones[] | "\(.id)\t\(.domain)"'`,
	"evo dns zone-get": `  evo dns zone-get <zone-id>
  evo dns zone-get <zone-id> --json`,
	"evo dns zone-create": `  evo dns zone-create example.com --dry-run
  evo dns zone-create example.com --yes
  evo dns zone-create corp.internal --type private --yes`,
	"evo dns zone-delete": `  evo dns zone-delete <zone-id> --dry-run
  evo dns zone-delete <zone-id>`,
	"evo dns records": `  evo dns records <zone-id>
  evo dns records <zone-id> --json | jq -r '.records[] | "\(.name)\t\(.type)"'`,
	"evo dns record-get": `  evo dns record-get <record-id>
  evo dns record-get <record-id> --json`,
	"evo dns record-create": `  evo dns record-create <zone-id> www --record-type a --value 203.0.113.10 --dry-run
  evo dns record-create <zone-id> api --record-type a --values 203.0.113.10,203.0.113.11 --ttl 60 --yes`,
	"evo dns record-delete": `  evo dns record-delete <record-id> --dry-run
  evo dns record-delete <record-id> --yes`,
	"evo dns operation": `  evo dns operation <operation-id>
  evo dns operation <operation-id> --wait-seconds 120`,

	// cert
	"evo cert list": `  evo cert list
  evo cert list --dns-name example.com
  evo cert list --state CERTIFICATE_STATE_EXPIRED --json`,
	"evo cert get": `  evo cert get <certificate-id>
  evo cert get <certificate-id> --json`,
	"evo cert issue-le": `  evo cert issue-le site --dns-names example.com,www.example.com --dry-run
  evo cert issue-le site --dns-names example.com --yes`,
	"evo cert import": `  evo cert import my-cert --server-certificate "$(base64 < cert.pem | tr -d '\n')" --private-key "$(base64 < key.pem | tr -d '\n')" --dry-run`,
	"evo cert renew": `  evo cert renew <certificate-id> --dry-run
  evo cert renew <certificate-id> --yes`,
	"evo cert delete": `  evo cert delete <certificate-id> --dry-run
  evo cert delete <certificate-id> --force --yes`,
	"evo cert usage": `  evo cert usage <certificate-id>`,
	"evo cert versions": `  evo cert versions <certificate-id>
  evo cert versions <certificate-id> --json`,
	"evo cert private-key": `  evo cert private-key <certificate-id>
  evo cert private-key <certificate-id> --version-id 2 --yes --json > key.json`,
	"evo cert operation": `  evo cert operation <operation-id>
  evo cert operation <operation-id> --wait-seconds 300`,

	// lb, tg
	"evo lb list": `  evo lb list
  evo lb list --json | jq -r '.load_balancers[] | "\(.id)\t\(.name)"'`,
	"evo lb get": `  evo lb get <lb-id>
  evo lb get <lb-id> --json`,
	"evo lb create": `  evo lb create web-lb --target-group-id <tg-id> --port 80 --dry-run
  evo lb create web-lb --target-group-id <tg-id> --port 443 --target-port 8443 --yes
  evo lb create db-lb --target-group-id <tg-id> --port 5432 --external=false --yes`,
	"evo lb delete": `  evo lb delete <lb-id> --dry-run
  evo lb delete <lb-id> --yes`,
	"evo lb operation": `  evo lb operation <operation-id>
  evo lb operation <operation-id> --wait-seconds 300`,
	"evo tg list": `  evo tg list
  evo tg list --json | jq -r '.target_groups[] | "\(.id)\t\(.name)"'`,
	"evo tg get": `  evo tg get <tg-id>`,
	"evo tg create": `  evo tg create web-tg --vm-id <vm-id> --dry-run
  evo tg create web-tg --targets '{"id":"<vm-id-1>"}' --targets '{"id":"<vm-id-2>"}' --yes`,
	"evo tg delete": `  evo tg delete <tg-id> --dry-run
  evo tg delete <tg-id> --yes`,

	// snat
	"evo snat list": `  evo snat list
  evo snat list --json`,
	"evo snat create": `  evo snat create nat-az2 --zone ru.AZ-2 --dry-run
  evo snat create nat-az2 --zone ru.AZ-2 --yes`,
	"evo snat delete": `  evo snat delete <snat-id> --dry-run
  evo snat delete <snat-id> --yes`,
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
