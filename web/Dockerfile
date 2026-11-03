# syntax=docker/dockerfile:1.7
# Image web : fichiers statiques servis par Caddy (non root, sans capacité).
# Cibles :
#   app          build Angular (nécessite web/package.json, livré par P1-14)
#   placeholder  page statique d'attente, utilisée tant que P1-14 n'est pas fusionnée (D70)
# La cible par défaut est « placeholder » ; `docker build --target app web/` produit l'image réelle.

# node:22-alpine
FROM node:22-alpine@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914 AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build -- --configuration production

# Caddy recompilé avec la chaîne Go de l'image de build et des dépendances corrigées (Trivy HIGH, D73) :
# le binaire de l'image officielle embarque une bibliothèque standard et golang.org/x/net en retard.
# Sans attribut de fichier (cap_net_bind_service), il tourne sans aucune capacité.
FROM golang:1.26-alpine@sha256:a2012b4a1d1f9f7e9f44e0d39b805a901e856b1dd80b4e6ee4338db547050308 AS caddy
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
WORKDIR /src
RUN go mod init caddybuild \
 && printf 'package main\nimport caddycmd "github.com/caddyserver/caddy/v2/cmd"\nimport _ "github.com/caddyserver/caddy/v2/modules/standard"\nfunc main() { caddycmd.Main() }\n' > main.go \
 && go get github.com/caddyserver/caddy/v2@v2.11.7 golang.org/x/net@latest github.com/go-jose/go-jose/v4@latest \
 && go mod tidy && go build -ldflags="-s -w" -o /out/caddy .

# caddy:2-alpine (système de base seulement ; mises à jour Alpine appliquées)
FROM caddy:2-alpine@sha256:e70b164da81ef698b3ad19af799ea6086ce6d9e48ed4079eb9fe26f6fbf14ee5 AS base
RUN apk upgrade --no-cache
COPY --from=caddy /out/caddy /usr/bin/caddy
COPY deploy/Caddyfile /etc/caddy/Caddyfile
USER 65532:65532
EXPOSE 8080

FROM base AS app
# Le chemin de sortie suit la config Angular (application builder : dist/<projet>/browser).
COPY --from=build /app/dist/*/browser /srv

FROM base AS placeholder
COPY deploy/placeholder/ /srv/

FROM placeholder
