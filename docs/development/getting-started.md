# Primeiros passos

## Pré-requisitos

- Docker Desktop com Docker Compose.
- Para rodar testes fora dos containers: Go 1.26 e Node.js 22.22.3 ou superior.

## Subir o ambiente

```bash
cp .env.example .env
docker compose up -d --build --wait
bash scripts/seed-dev.sh
```

O último comando cria duas lojas de demonstração, cada uma com categorias, produtos, estoque e opções de frete, e pode ser repetido sem efeito colateral. Na primeira execução, ele mostra o e-mail e a senha aleatória da equipe de demonstração de cada loja, para entrar no admin.

### Compra de demonstração

O ambiente local usa o **Pagamento de teste**, identificado como tal no checkout e no pedido: nenhum valor é cobrado. Na página do pedido, o comprador aprova ou recusa o pagamento, e a resposta chega à API como um webhook assinado, pelo mesmo caminho de um gateway real. Os e-mails do pedido aparecem no Mailpit alguns segundos depois, enviados pelo worker. No admin da loja, a equipe acompanha o pedido em **Pedidos** e avança a separação e o envio. Um pedido não pago é cancelado automaticamente após `ORDER_PAYMENT_TTL`, e o estoque reservado volta a ficar disponível.

| Serviço | Endereço |
| --- | --- |
| Loja Aurora (demonstração) | <http://aurora.localhost:8080> |
| Loja Terra (demonstração) | <http://terra.localhost:8080> |
| Admin da loja | <http://admin.localhost:8080> |
| Console da plataforma | <http://console.localhost:8080> |
| API | <http://localhost:8080/api/v1/status> |
| E-mails de desenvolvimento (Mailpit) | <http://localhost:8025> |
| Grafana (usuário `admin`, senha em `.env`) | <http://localhost:3000> |
| Prometheus | <http://localhost:9090> |
| RabbitMQ (usuário `ecommerce`, senha em `.env`) | <http://localhost:15672> |

PostgreSQL e Redis ficam acessíveis somente pela rede interna do Docker. Para abrir um terminal no banco:

```bash
docker compose exec postgres psql -U postgres -d ecommerce
```

Para parar, use `docker compose down`. Para apagar também os dados locais, use `docker compose down -v`.

## Worker e mensageria

O serviço `worker` publica os eventos do outbox no RabbitMQ, consome os eventos, envia os e-mails e cancela pedidos vencidos. Sem ele, a loja funciona, mas e-mails e estornos ficam pendentes até ele voltar.

```bash
docker compose logs -f worker
docker compose exec worker /app/worker dlq list --consumer sales-notifications
```

As filas podem ser vistas no painel do RabbitMQ. Para reprocessar mensagens mortas, veja [Arquitetura orientada a eventos](../architecture/event-driven-architecture.md#6-implementado-na-fase-6).

## Verificar o ambiente

```bash
bash scripts/smoke.sh
```

O script confere a API, as três aplicações, os cabeçalhos de segurança e a chegada de métricas, logs e traces.

Para conferir em um navegador real a identidade de cada loja, a compra completa e o atendimento do pedido:

```bash
cd frontend
npx playwright install chromium
npm run test:e2e
```

## Gerenciar lojas

Lojas são gerenciadas pela ferramenta `tenantctl`:

```bash
docker compose run --rm tenantctl tenant list
docker compose run --rm tenantctl tenant create --slug minha-loja --name "Minha Loja" --plan growth
docker compose run --rm tenantctl settings set --slug minha-loja --seo-description "Descrição da loja"
docker compose run --rm tenantctl feature set --slug minha-loja --key wishlist --enabled=false
docker compose run --rm tenantctl theme publish --slug minha-loja --file /seed/aurora.json
docker compose run --rm tenantctl theme rollback --slug minha-loja --version 1
docker compose run --rm tenantctl tenant suspend --slug minha-loja
```

A loja criada fica disponível em `http://minha-loja.localhost:8080`. Alterações de status e de domínio levam até 30 segundos para valer.

Para usar um domínio próprio, registre-o, crie o registro DNS TXT informado e confirme:

```bash
docker compose run --rm tenantctl domain add --slug minha-loja --host www.minhaloja.com.br
docker compose run --rm tenantctl domain verify --host www.minhaloja.com.br
```

Os arquivos de tema ficam em `deployments/compose/seed`, montados em `/seed` no container. O backend recusa cores com contraste insuficiente, valores fora das listas permitidas e qualquer campo desconhecido.

## Criar os primeiros acessos

Compradores se cadastram na própria loja. Equipes e operadores entram por convite:

```bash
docker compose run --rm tenantctl staff invite --slug aurora --email dona@exemplo.com --name "Dona da Loja"
docker compose run --rm tenantctl operator invite --email operador@exemplo.com --name "Operador"
```

O comando mostra o link do convite, válido por 7 dias. No painel da loja, informe o identificador da loja (`aurora`) ao entrar. No console da plataforma, a verificação em duas etapas é obrigatória.

Em desenvolvimento, nenhum e-mail sai do ambiente: as mensagens ficam disponíveis no Mailpit.

## Backend

```bash
cd backend
go test ./...                                          # Testes unitários
go test -tags integration -count=1 ./tests/...         # Integração, requer Docker
golangci-lint run ./...                                # Lint
```

Para criar uma migration, adicione um arquivo em `backend/migrations` com o próximo número da sequência. Cada arquivo precisa das seções `-- +goose Up` e `-- +goose Down`.

Depois de alterar consultas SQL, regenere o código:

```bash
cd backend
docker run --rm -v "$PWD:/src" -w /src sqlc/sqlc:1.31.1 generate
```

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
- `AUTH_COOKIE_SECURE=true`, `RATE_LIMIT_ENABLED=true`, `SMTP_REQUIRE_TLS=true` e `STORE_PUBLIC_SCHEME=https`.
- `PAYMENTS_TEST_PROVIDER=false` em `production`.
- Chaves diferentes das chaves de exemplo do `.env.example`.

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
| `PLATFORM_DB_USER`, `PLATFORM_DB_PASSWORD` | obrigatórias no `tenantctl` | Credencial usada só para gerenciar lojas |
| `PLATFORM_BASE_DOMAIN` | obrigatória no `tenantctl` | Domínio base dos subdomínios das lojas |
| `AUTH_SIGNING_KEY`, `MFA_ENCRYPTION_KEY` | obrigatórias | Chaves de 32 bytes em base64. Gere com `openssl rand -base64 32` |
| `AUTH_COOKIE_SECURE` | `true` | Exige HTTPS no cookie de sessão |
| `TRUSTED_PROXIES` | vazia | Faixas CIDR dos proxies confiáveis |
| `RATE_LIMIT_ENABLED` | `true` | Liga os limites de tentativas |
| `ADMIN_PUBLIC_URL`, `CONSOLE_PUBLIC_URL` | obrigatórias | Endereços usados nos e-mails |
| `STORE_PUBLIC_SCHEME`, `STORE_PUBLIC_PORT` | `https`, vazia | Endereço das lojas nos e-mails |
| `SMTP_ADDR`, `SMTP_FROM` | obrigatórias | Servidor e remetente de e-mail |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | vazias | Credenciais do servidor de e-mail |
| `SMTP_REQUIRE_TLS` | `true` | Exige TLS no envio de e-mail |
| `REDIS_ADDR` | obrigatória | Endereço do Redis |
| `REDIS_PASSWORD`, `REDIS_TLS` | vazia, `false` | Acesso ao Redis |
| `PAYMENTS_TEST_PROVIDER` | `false` | Habilita o Pagamento de teste. Proibido em produção |
| `ORDER_PAYMENT_TTL` | `30m` | Prazo para pagar antes do cancelamento automático |
| `ORDER_EXPIRY_INTERVAL` | `30s` | Intervalo da rotina que cancela pedidos vencidos |
| `RABBITMQ_HOST`, `RABBITMQ_USER`, `RABBITMQ_PASSWORD` | obrigatórias no worker | Acesso ao RabbitMQ |
| `RABBITMQ_PORT`, `RABBITMQ_VHOST`, `RABBITMQ_TLS` | `5672`, vazio, `false` | TLS é obrigatório em `staging` e `production` |
| `WORKER_HEALTH_ADDR` | `:8081` | Health check do worker |
| `MESSAGING_RELAY_INTERVAL` | `500ms` | Intervalo de leitura do outbox |
| `MAIL_DISPATCH_INTERVAL` | `2s` | Intervalo de envio de e-mails |
| `MESSAGING_RETENTION` | `168h` | Tempo de guarda de eventos publicados e processados |
| `TELEMETRY_ENABLED` | `true` | Liga a exportação de telemetria |
| `TELEMETRY_OTLP_ENDPOINT` | `otel-collector:4317` | Destino OTLP |
| `TELEMETRY_OTLP_INSECURE` | `false` | Permite OTLP sem TLS |

## Ver um trace no Grafana

1. Faça uma requisição: `curl http://localhost:8080/api/v1/status`.
2. Abra **Explore** no Grafana e selecione a fonte **Tempo**.
3. Pesquise pelo serviço `ecommerce-api` e abra o trace `GET /api/v1/status`.
4. Use o `trace_id` para encontrar os logs correspondentes na fonte **Loki**.
