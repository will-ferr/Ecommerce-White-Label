# Ecommerce White Label

Plataforma SaaS de e-commerce multi-tenant: um único backend e um único frontend atendem várias lojas isoladas, cada uma com identidade própria.

**Estado atual:** Fase 7 (administração). Cada loja tem domínio, tema, equipe, catálogo com busca, estoque, carrinho, checkout com cupons, pedidos, frete por tabela, páginas de conteúdo, relatórios e e-mails próprios. Lojas são criadas e acompanhadas pelo console da plataforma, e clientes baixam e excluem os próprios dados. Eventos passam por outbox e RabbitMQ, e pagamentos são confirmados por webhooks assinados. O único provedor de pagamento ainda é o de teste, identificado como tal: o gateway real depende da escolha do provedor.

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
