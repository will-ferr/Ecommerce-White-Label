# Visão geral do sistema

Status: Fase 1, proposta para aprovação. As decisões citadas como ADR estão em [architecture-decisions.md](architecture-decisions.md).

## 1. Objetivo e princípios

A plataforma é um SaaS de e-commerce White Label: **um único backend e um único código de frontend** atendem várias lojas isoladas. Toda diferença entre lojas vem de configuração, temas, feature flags e pontos de extensão controlados. Não existem forks por cliente.

| Princípio | Consequência prática |
| --- | --- |
| Isolamento é garantido em camadas | Aplicação, banco (RLS), cache, filas e armazenamento aplicam o tenant de forma independente |
| O servidor é a fonte da verdade | Preço, desconto, frete, estoque e tenant são sempre recalculados ou validados no backend |
| Fronteiras antes de distribuição | Monólito modular com fronteiras impostas pelo compilador; extração só com evidência |
| Falhar fechado | Sem contexto de tenant, o banco devolve zero linhas |
| Observável desde o primeiro endpoint | Logs, métricas e traces fazem parte da Fase 2, não da Fase 8 |
| Simplicidade justificada | Toda abstração precisa de um segundo uso real ou de uma fronteira de teste |

## 2. C4 nível 1: contexto

```mermaid
flowchart TB
    buyer["Comprador<br/>[Pessoa]<br/>Compra na loja de um tenant"]
    staff["Equipe da loja<br/>[Pessoa]<br/>Gerencia catálogo, pedidos e marca"]
    super["Super Admin<br/>[Pessoa]<br/>Opera o SaaS"]

    platform["Plataforma E-commerce White Label<br/>[Sistema]<br/>Lojas isoladas sobre backend compartilhado"]

    pay["Gateways de pagamento<br/>[Externo]<br/>Mercado Pago, Stripe, Pagar.me"]
    ship["Transportadoras<br/>[Externo]<br/>Correios, Melhor Envio, Jadlog"]
    mail["E-mail transacional<br/>[Externo]"]
    fiscal["Emissor fiscal<br/>[Externo, futuro]<br/>NF-e e NFC-e"]
    mkt["Analytics e marketing<br/>[Externo]"]
    dns["DNS e CDN<br/>[Externo]<br/>Domínios e certificados"]

    buyer -->|"HTTPS, domínio da loja"| platform
    staff -->|"HTTPS, painel da loja"| platform
    super -->|"HTTPS, painel da plataforma"| platform
    platform -->|"Cobranças, estornos"| pay
    pay -->|"Webhooks assinados"| platform
    platform -->|"Cotação, etiqueta, rastreio"| ship
    ship -->|"Atualização de status"| platform
    platform -->|"Envia mensagens"| mail
    platform -.->|"Documentos fiscais"| fiscal
    platform -->|"Eventos consentidos"| mkt
    dns -->|"Roteia domínios"| platform
```

## 3. C4 nível 2: containers

```mermaid
flowchart TB
    browser["Navegador"]

    subgraph edge["Borda"]
        cdn["CDN e WAF"]
        ingress["Ingress<br/>TLS, roteamento por host"]
    end

    subgraph apps["Aplicações"]
        store["Storefront<br/>[Nuxt, Vue 3, SSR]"]
        admin["Admin da loja<br/>[Vue 3, SPA]"]
        padmin["Admin da plataforma<br/>[Vue 3, SPA]"]
        api["API<br/>[Go, REST]<br/>Monólito modular"]
        worker["Worker<br/>[Go]<br/>Consumidores e jobs"]
        relay["Outbox Relay<br/>[Go]"]
    end

    subgraph data["Dados"]
        pgb["PgBouncer"]
        pg[("PostgreSQL<br/>Dados e RLS")]
        redis[("Redis<br/>Cache e rate limit")]
        mq[["RabbitMQ<br/>Eventos e filas"]]
        s3[("Object Storage<br/>Mídia")]
    end

    subgraph obs["Observabilidade"]
        otel["OTel Collector"]
        lgtm["Prometheus, Loki,<br/>Tempo e Grafana"]
    end

    ext["Gateways, transportadoras,<br/>e-mail"]

    browser --> cdn --> ingress
    ingress -->|"páginas da loja"| store
    ingress -->|"admin.*"| admin
    ingress -->|"console.*"| padmin
    ingress -->|"/api/*"| api
    store -->|"REST interno"| api
    api --> pgb
    worker --> pgb
    relay --> pgb
    pgb --> pg
    api --> redis
    worker --> redis
    api --> s3
    relay -->|"publica"| mq
    mq -->|"consome"| worker
    api --> ext
    worker --> ext
    ext -->|"webhooks"| ingress
    api -.-> otel
    worker -.-> otel
    relay -.-> otel
    store -.-> otel
    otel -.-> lgtm
```

API, Worker e Relay são **o mesmo código Go** com pontos de entrada diferentes (`cmd/api`, `cmd/worker`, `cmd/relay`). Eles escalam de forma independente, sem virar microsserviços.

## 4. C4 nível 3: componentes do fluxo de compra

```mermaid
flowchart LR
    mw["Middlewares<br/>trace, tenant, auth, rate limit"]
    subgraph checkout["Módulo checkout"]
        h["Handler HTTP"]
        uc["Caso de uso<br/>PlaceOrder"]
    end
    cart["cart"]
    pricing["pricing"]
    promo["promotions"]
    ship["shipping"]
    tax["fiscal"]
    inv["inventory"]
    orders["orders"]
    pay["payments"]
    outbox[("outbox")]
    gw["Adaptador do gateway"]

    mw --> h --> uc
    uc -->|"lê itens"| cart
    uc -->|"recalcula"| pricing
    uc -->|"valida"| promo
    uc -->|"cota"| ship
    uc -->|"calcula"| tax
    uc -->|"reserva"| inv
    uc -->|"cria"| orders
    orders --> outbox
    uc -->|"após o commit"| pay
    pay --> gw
```

## 5. C4 nível 4: implantação alvo

```mermaid
flowchart TB
    user["Usuários"] --> cf["CDN e WAF"]
    cf --> lb["Load Balancer"]
    subgraph k8s["Cluster Kubernetes, múltiplas zonas"]
        ing["Ingress Controller"]
        subgraph appns["Namespace app"]
            sapi["api, HPA"]
            sstore["storefront, HPA"]
            sadmin["admin estático"]
            sworker["worker, HPA por fila"]
            srelay["relay, 2 réplicas"]
            bouncer["pgbouncer"]
        end
        subgraph obsns["Namespace observability"]
            col["otel-collector"]
            stack["prometheus, loki, tempo, grafana"]
        end
    end
    subgraph managed["Serviços gerenciados"]
        mpg[("PostgreSQL HA com PITR")]
        mredis[("Redis HA")]
        mmq[["RabbitMQ em cluster"]]
        ms3[("Object Storage")]
        kms["KMS e Secrets Manager"]
    end
    lb --> ing
    ing --> sapi
    ing --> sstore
    ing --> sadmin
    sapi --> bouncer --> mpg
    sworker --> bouncer
    srelay --> bouncer
    sapi --> mredis
    srelay --> mmq --> sworker
    sapi --> ms3
    sapi --> kms
    sapi -.-> col --> stack
```

O provedor de nuvem ainda não foi escolhido. O desenho usa apenas capacidades disponíveis nos principais provedores.

## 6. Tecnologias e justificativas

As versões exatas serão fixadas na Fase 2.

| Área | Escolha | Por que | Alternativa descartada |
| --- | --- | --- | --- |
| Linguagem | Go | Requisito; binário único, concorrência simples | n/a |
| HTTP | `net/http` com chi | Compatível com a biblioteca padrão, sem framework invasivo | Gin, Fiber: API própria ou fora do `net/http` |
| Acesso a dados | pgx com sqlc | SQL explícito e tipado, essencial para RLS e consultas auditáveis | GORM: esconde SQL e dificulta revisar o isolamento |
| Migrations | goose | Migrations SQL versionadas, simples de rodar em pipeline | golang-migrate: equivalente, menos flexível em Go |
| Banco | PostgreSQL 18 | RLS maduro, transações, JSONB, busca textual | n/a |
| Pool | PgBouncer em modo transação | Protege o limite de conexões; compatível com `set_config` local | Conexão direta: esgota com escala horizontal |
| Cache | Redis | Cache, rate limit e dados temporários; nunca fonte da verdade | n/a |
| Mensageria | RabbitMQ | Filas de trabalho, DLQ e retentativas por mensagem (ADR-008) | Kafka: custo operacional sem necessidade de replay |
| Busca | PostgreSQL FTS, depois OpenSearch | Começa sem nova infraestrutura, atrás de uma porta (ADR-011) | OpenSearch desde o início: custo prematuro |
| Storefront | Nuxt (Vue 3, Pinia, Vue Router, Tailwind) | SEO exige HTML no servidor (ADR-006, pendente) | SPA pura: prejudica SEO e causa flash de tema |
| Painéis | Vue 3, Vite, Pinia, Vue Router, Tailwind | Área autenticada não precisa de SSR | n/a |
| Telemetria | OpenTelemetry, Prometheus, Loki, Tempo, Grafana | Requisito; padrão aberto e sem aprisionamento | n/a |
| Logs | `log/slog` em JSON | Biblioteca padrão, sem dependência extra | zap, zerolog: ganho não justificado agora |
| Testes | Go testing, Testcontainers, Vitest, Playwright, k6, ZAP | Requisito; banco real nos testes de isolamento | Mocks de banco: não exercitam RLS |

## 7. Estrutura inicial de diretórios

```text
.
├── backend/
│   ├── cmd/
│   │   ├── api/                  # Servidor HTTP
│   │   ├── worker/               # Consumidores e jobs
│   │   ├── relay/                # Publicação do outbox
│   │   └── migrate/              # Migrations
│   ├── internal/
│   │   ├── app/                  # Composition root: conecta os módulos
│   │   ├── platform/             # config, database, cache, messaging,
│   │   │                         # middleware, observability, security
│   │   ├── modules/
│   │   │   └── catalog/          # Mesmo formato em todos os módulos
│   │   │       ├── module.go     # Registro de rotas, consumidores e dependências
│   │   │       ├── contract/     # Única parte importável por outros módulos
│   │   │       └── internal/     # Invisível para os demais, garantido pelo compilador
│   │   │           ├── domain/   # Entidades e regras, sem infraestrutura
│   │   │           ├── app/      # Casos de uso e portas
│   │   │           └── adapters/ # http, postgres, events, gateways
│   │   └── shared/               # errors, events, pagination, money
│   ├── migrations/               # Uma pasta por módulo
│   ├── api/                      # OpenAPI usado na geração e nos contract tests
│   ├── tests/                    # integration, isolation, concurrency, contract
│   ├── Dockerfile
│   ├── Makefile
│   └── go.mod
├── frontend/
│   ├── apps/
│   │   ├── storefront/           # Loja (SSR)
│   │   ├── admin/                # Admin da loja
│   │   └── console/              # Admin da plataforma
│   └── packages/
│       ├── ui/                   # Design system e tokens CSS
│       ├── theme/                # Validação e aplicação de temas
│       ├── api-client/           # Cliente gerado a partir do OpenAPI
│       └── config/               # ESLint, TypeScript, Tailwind
├── deployments/
│   ├── compose/                  # Ambiente de desenvolvimento
│   ├── kubernetes/               # Base e overlays por ambiente
│   └── observability/            # Dashboards, alertas e Collector
├── tests/                        # e2e, load (k6), security (ZAP)
├── docs/
└── .github/workflows/
```

Diferenças em relação à estrutura sugerida na especificação:

- **`contract/` e `internal/` em cada módulo** fazem o compilador Go bloquear acesso indevido entre módulos.
- **`cmd/worker` e `cmd/relay`** permitem escalar o processamento assíncrono separadamente da API.
- **`shared/utils` foi removido**, porque pacotes genéricos tendem a acumular regras de negócio.
- **`deployments/` fica na raiz**, porque o Compose e o Kubernetes cobrem backend e frontend.
- **`frontend/` virou um monorepo** com três aplicações e pacotes compartilhados. Depende da aprovação do ADR-006.
