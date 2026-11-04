#!/usr/bin/env bash
# P1-24 : Trivy (HIGH/CRITICAL, correctif disponible) sur les images tierces de la pile, sans reconstruire api/web/backup
# (`make scan-images` couvre toute la pile, mais construit les images). Politique de D290 : mêmes seuils, même image Trivy
# (lue dans le Makefile), exceptions uniquement par `.trivyignore` (--ignorefile, échéances `exp:` comprises).
# Les images sont celles de deploy/compose.yaml (Docker requis ; la base Trivy est téléchargée au premier appel).
# Usage : e2e/gate/trivy_tierces.sh   (code 1 s'il reste des vulnérabilités)
set -uo pipefail
sortie="$(mktemp -d)"
trap 'rm -rf "$sortie"' EXIT
chmod 777 "$sortie"
trivy="$(sed -n 's/^TRIVY_IMAGE *[:?]*= *//p' Makefile | head -1)"
[[ -n "$trivy" ]] || { echo "TRIVY_IMAGE introuvable dans le Makefile" >&2; exit 2; }
# Les images sont lues dans le fichier Compose (une seule source de vérité).
images="$(sed -n 's/^ *image: *\(traefik:[^ ]*\|quay.io[^ ]*\|postgres:[^ ]*\).*/\1/p' deploy/compose.yaml | sort -u)"
total=0
for img in $images; do
	rm -f "$sortie/r.json"
	docker run --rm -e HOME=/tmp -v /var/run/docker.sock:/var/run/docker.sock -v "$sortie:/out" -v "$PWD/.trivyignore:/exceptions/.trivyignore:ro" "$trivy" \
		image --quiet --severity HIGH,CRITICAL --ignore-unfixed --ignorefile /exceptions/.trivyignore --format json -o /out/r.json "$img" >/dev/null 2>&1
	[[ -s "$sortie/r.json" ]] || { echo "$img : scan impossible (pas de rapport)" >&2; exit 2; }
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
