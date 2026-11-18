#!/usr/bin/env bash
# Creates two demonstration stores for local development and CI.
set -euo pipefail

# Git Bash on Windows would otherwise rewrite the paths passed to the container.
export MSYS_NO_PATHCONV=1

ctl() { docker compose run --rm --no-deps -T tenantctl "$@"; }

seed() { # slug, name, plan, SEO description
  if ctl tenant exists --slug "$1"; then
    echo "Loja $1 já existe"
    return
  fi
  ctl tenant create --slug "$1" --name "$2" --plan "$3"
  ctl settings set --slug "$1" --seo-title "$2" --seo-description "$4"
  ctl theme publish --slug "$1" --file "/seed/$1.json"
}

seed aurora "Aurora Cosméticos" enterprise "Loja de demonstração com identidade delicada e layout centralizado."
seed terra "Terra Café" starter "Loja de demonstração com identidade rústica e layout compacto."
