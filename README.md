# Ecommerce White Label

Plataforma SaaS de e-commerce multi-tenant: um único backend e um único frontend atendem várias lojas isoladas, cada uma com identidade própria.

**Estado atual:** Fase 5 (núcleo comercial). Cada loja tem domínio, tema, contas, equipe, catálogo, estoque, carrinho, checkout e pedidos próprios. O pagamento ainda é o provedor de teste, identificado como tal. Gateways e transportadoras reais chegam na Fase 6.

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
