# Primeiros passos

## Pré-requisitos

- Docker Desktop com Docker Compose.
- Para rodar testes fora dos containers: Go 1.26 e Node.js 22.22.3 ou superior.

## Subir o ambiente

```bash
cp .env.example .env
docker compose up -d --build --wait
```

| Serviço | Endereço |
| --- | --- |
| Loja | <http://loja-teste.localhost:8080> |
| Admin da loja | <http://admin.localhost:8080> |
| Console da plataforma | <http://console.localhost:8080> |
| API | <http://localhost:8080/api/v1/status> |
| Grafana (usuário `admin`, senha em `.env`) | <http://localhost:3000> |
| Prometheus | <http://localhost:9090> |
| RabbitMQ (usuário `ecommerce`, senha em `.env`) | <http://localhost:15672> |

PostgreSQL e Redis ficam acessíveis somente pela rede interna do Docker. Para abrir um terminal no banco:

```bash
docker compose exec postgres psql -U postgres -d ecommerce
```

Para parar, use `docker compose down`. Para apagar também os dados locais, use `docker compose down -v`.

## Verificar o ambiente

```bash
bash scripts/smoke.sh
```

O script confere a API, as três aplicações, os cabeçalhos de segurança e a chegada de métricas, logs e traces.

## Backend

```bash
cd backend
go test ./...                                          # Testes unitários
go test -tags integration -count=1 ./tests/...         # Integração, requer Docker
golangci-lint run ./...                                # Lint
```

Para criar uma migration, adicione um arquivo em `backend/migrations` com o próximo número da sequência. Cada arquivo precisa das seções `-- +goose Up` e `-- +goose Down`.

## Frontend

```bash
cd frontend
npm ci
npm run lint
npm run typecheck
npm test
npm run build
```

Depois de alterar `docs/api/openapi.yaml`, execute `npm run generate:api` para atualizar os tipos do cliente. O CI falha se os tipos estiverem desatualizados.

## Configuração por ambiente

`APP_ENV` aceita `development`, `testing`, `staging` e `production`. A API recusa iniciar com configuração inválida e informa todos os problemas de uma vez.

Em `staging` e `production`, a API também exige:

- `DB_SSLMODE` igual a `verify-full` ou `verify-ca`.
- `REDIS_TLS=true` e `REDIS_PASSWORD` preenchido.
- `TELEMETRY_OTLP_INSECURE=false`.

A API também recusa iniciar se o usuário do banco for superusuário ou tiver `BYPASSRLS`.

| Variável | Padrão | Descrição |
| --- | --- | --- |
| `APP_ENV` | obrigatória | Ambiente de execução |
| `LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN` ou `ERROR` |
| `HTTP_ADDR` | `:8080` | Endereço do servidor |
| `HTTP_REQUEST_TIMEOUT` | `15s` | Tempo máximo de uma requisição |
| `HTTP_MAX_BODY_BYTES` | `1048576` | Tamanho máximo do corpo |
| `HTTP_DRAIN_DELAY` | `0s` | Espera antes de encerrar, para o balanceador remover a instância |
| `DB_HOST`, `DB_NAME` | obrigatórias | Localização do PostgreSQL |
| `DB_PORT` | `5432` | Porta do PostgreSQL |
| `DB_USER`, `DB_PASSWORD` | obrigatórias | Credencial de execução |
| `DB_SSLMODE` | `verify-full` | Modo TLS do PostgreSQL |
| `DB_MAX_CONNS`, `DB_MIN_CONNS` | `20`, `2` | Limites do pool |
| `MIGRATION_DB_USER`, `MIGRATION_DB_PASSWORD` | obrigatórias nas migrations | Credencial usada só para migrations |
| `REDIS_ADDR` | obrigatória | Endereço do Redis |
| `REDIS_PASSWORD`, `REDIS_TLS` | vazia, `false` | Acesso ao Redis |
| `TELEMETRY_ENABLED` | `true` | Liga a exportação de telemetria |
| `TELEMETRY_OTLP_ENDPOINT` | `otel-collector:4317` | Destino OTLP |
| `TELEMETRY_OTLP_INSECURE` | `false` | Permite OTLP sem TLS |

## Ver um trace no Grafana

1. Faça uma requisição: `curl http://localhost:8080/api/v1/status`.
2. Abra **Explore** no Grafana e selecione a fonte **Tempo**.
3. Pesquise pelo serviço `ecommerce-api` e abra o trace `GET /api/v1/status`.
4. Use o `trace_id` para encontrar os logs correspondentes na fonte **Loki**.
