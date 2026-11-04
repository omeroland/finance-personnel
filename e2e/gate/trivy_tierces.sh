#!/usr/bin/env bash
# P1-24 : Trivy (HIGH/CRITICAL, correctif disponible) sur les images tierces de la pile, que `make scan-images`
# ne couvre pas (il ne scanne que finance/api:dev et finance/web:dev). Même image Trivy, épinglée par digest.
# Les images sont celles de deploy/compose.yaml (Docker requis ; la base Trivy est téléchargée au premier appel).
# Usage : e2e/gate/trivy_tierces.sh   (code 1 s'il reste des vulnérabilités)
set -uo pipefail
sortie="$(mktemp -d)"
trap 'rm -rf "$sortie"' EXIT
chmod 777 "$sortie"
trivy='aquasec/trivy:latest@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c'
# Les images sont lues dans le fichier Compose (une seule source de vérité).
images="$(sed -n 's/^ *image: *\(traefik:[^ ]*\|quay.io[^ ]*\|postgres:[^ ]*\).*/\1/p' deploy/compose.yaml | sort -u)"
total=0
for img in $images; do
	docker run --rm -e HOME=/tmp -v /var/run/docker.sock:/var/run/docker.sock -v "$sortie:/out" "$trivy" \
		image --quiet --severity HIGH,CRITICAL --ignore-unfixed --format json -o /out/r.json "$img" >/dev/null 2>&1
	n="$(python3 -I -c 'import json;d=json.load(open("'"$sortie"'/r.json"));print(sum(len(r.get("Vulnerabilities") or []) for r in d.get("Results",[])))')"
	echo "$img : $n vulnérabilité(s) HIGH/CRITICAL avec correctif"
	python3 -I -c '
import json
d = json.load(open("'"$sortie"'/r.json"))
for r in d.get("Results", []):
    v = r.get("Vulnerabilities") or []
    if v:
        print("   ", r["Target"][:60], len(v), sorted({x["PkgName"] + " " + x["InstalledVersion"] for x in v})[:3])
'
	total=$((total + n))
done
[[ "$total" -eq 0 ]]
