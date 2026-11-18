# Ecommerce White Label

Plataforma SaaS de e-commerce multi-tenant: um único backend e um único frontend atendem várias lojas isoladas, cada uma com identidade própria.

**Estado atual:** Fase 3 (núcleo multi-tenant). Lojas já têm domínio, tema e funcionalidades próprios, mas a plataforma ainda não possui login, catálogo ou checkout.

| Camada | Tecnologias |
| --- | --- |
| Backend | Go, PostgreSQL, Redis, RabbitMQ |
| Frontend | Vue 3, Nuxt, Pinia, Tailwind CSS |
| Observabilidade | OpenTelemetry, Prometheus, Loki, Tempo, Grafana |

## Como executar

```bash
cp .env.example .env
docker compose up -d --build --wait
bash scripts/seed-dev.sh
```

Depois, acesse as lojas de demonstração <http://aurora.localhost:8080> e <http://terra.localhost:8080>.

- [Primeiros passos](docs/development/getting-started.md)
- [Documentação de arquitetura](docs/README.md)
