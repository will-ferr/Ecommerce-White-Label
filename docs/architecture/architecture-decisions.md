# Registro de decisões arquiteturais (ADRs)

Cada decisão registra contexto, escolha, consequências e alternativas. Uma decisão aceita só muda por um novo ADR que a substitua.

| ADR | Decisão | Status |
| --- | --- | --- |
| 001 | Monólito modular com fronteiras impostas pelo compilador | Aceito |
| 002 | SQL explícito com pgx e sqlc | Aceito |
| 003 | Banco compartilhado com RLS | Aceito |
| 004 | Um schema PostgreSQL por módulo | Aceito |
| 005 | Tenant resolvido pelo Host e API na mesma origem | Aceito |
| 006 | Storefront com SSR em Nuxt e painéis em SPA | Aceito |
| 007 | Access token curto e refresh token opaco rotativo | Aceito |
| 008 | RabbitMQ com Outbox | Aceito |
| 009 | Checkout em transação local, gateway fora dela | Aceito |
| 010 | Dinheiro em inteiros e identificadores UUIDv7 | Aceito |
| 011 | Busca em PostgreSQL atrás de uma porta | Aceito |
| 012 | Dados de cartão nunca passam pela plataforma | Aceito |
| 013 | Temas por tokens validados | Aceito |
| 014 | Segredos de tenant com envelope encryption | Aceito |

## ADR-001: Monólito modular com fronteiras impostas pelo compilador

**Contexto.** O produto tem cerca de vinte domínios e uma equipe pequena. Microsserviços agora trariam custo operacional sem benefício concreto.

**Decisão.** Um único módulo Go. Cada domínio expõe apenas o pacote `contract/`, e sua implementação fica em `internal/`, que o compilador impede outros módulos de importar. Chamadas síncronas usam interfaces de `contract/`, e reações assíncronas usam eventos.

**Consequências.** Deploy simples e refatoração segura. Um módulo pode ser extraído trocando o adaptador da chamada por HTTP ou gRPC. Exige disciplina para manter os contratos pequenos.

**Alternativas.** Microsserviços (descartado por custo) e monólito em camadas (descartado por não proteger fronteiras).

## ADR-002: SQL explícito com pgx e sqlc

**Contexto.** O isolamento entre tenants precisa ser revisável em cada consulta.

**Decisão.** Consultas escritas em SQL e compiladas para Go com sqlc, executadas com pgx.

**Consequências.** Consultas tipadas e auditáveis. Filtros dinâmicos, como os do catálogo, exigem um construtor de consultas pequeno e parametrizado.

**Alternativas.** GORM ou ent: produtividade inicial maior, mas SQL gerado e menor controle.

## ADR-003: Banco compartilhado com RLS

**Contexto.** O SaaS precisa de custo por loja baixo e operação única, sem abrir mão do isolamento.

**Decisão.** Um banco com `tenant_id` em toda tabela de loja, RLS com `FORCE`, papéis sem `BYPASSRLS` e chaves estrangeiras compostas. Detalhes em [multi-tenancy.md](multi-tenancy.md).

**Consequências.** Um bug na aplicação não expõe outra loja. Exige cuidado com vizinhos barulhentos e com a restauração de um único tenant.

**Alternativas.** Banco por tenant (operação cara, migrations em N bancos) e schema por tenant (catálogo do PostgreSQL cresce demais). Um tenant muito grande poderá ser movido para um cluster próprio no futuro, pois o código não depende de onde o tenant está.

## ADR-004: Um schema PostgreSQL por módulo

**Contexto.** Tabelas de módulos diferentes não devem se acoplar, para permitir extração futura.

**Decisão.** Cada módulo tem um schema (`catalog`, `orders`, etc.). Chaves estrangeiras valem dentro do módulo e para `core.tenants`. Entre módulos, a referência é por ID, e dados históricos são copiados como snapshot, como nome e preço no item do pedido. Consultas com `JOIN` entre módulos são proibidas.

**Consequências.** Módulos extraíveis e pedidos historicamente corretos. A integridade entre módulos passa a ser garantida pelos casos de uso e por eventos.

## ADR-005: Tenant resolvido pelo Host e API na mesma origem

**Contexto.** Navegadores restringem cookies de terceiros, e cada loja pode ter domínio próprio.

**Decisão.** Cada loja acessa a API em `https://dominio-da-loja/api/v1`. O Ingress encaminha o caminho `/api` para o backend, e o tenant vem do `Host` validado contra domínios verificados.

**Consequências.** O storefront não precisa de CORS, os cookies são primários e o frontend nunca informa o tenant. O Ingress precisa emitir certificados para domínios de clientes.

**Alternativas.** API central em `api.plataforma.com` com cabeçalho de tenant: depende de CORS, de cookies de terceiros e de um valor controlado pelo cliente.

## ADR-006: Storefront com SSR em Nuxt e painéis em SPA

**Contexto.** A especificação pede Vue 3 e sugere uma aplicação única, mas também exige SEO por tenant, Open Graph, dados estruturados e alta conversão. Robôs de redes sociais não executam JavaScript, então uma SPA pura não entrega Open Graph por produto.

**Decisão.** Storefront em Nuxt, que usa Vue 3, TypeScript, Composition API, Pinia, Vue Router e Tailwind. Painéis em SPA com Vite. Código compartilhado em pacotes de um monorepo.

| Opção | Vantagens | Desvantagens |
| --- | --- | --- |
| **Nuxt no storefront, SPA nos painéis (recomendada)** | SEO e Open Graph completos, tema aplicado sem flash, melhor LCP no celular | Runtime Node em produção, cuidado com cache por host |
| SPA única com Vite | Aderente à estrutura sugerida, deploy estático | SEO limitado, flash de tema, carregamento inicial pior |
| SPA com pré-renderização para robôs | Mantém a SPA | Duas versões da página para manter, conteúdo pode divergir |

## ADR-007: Access token curto e refresh token opaco rotativo

**Decisão.** Access token JWT assinado com EdDSA, válido por 10 minutos, com `sub`, `tid`, `realm`, `sid` e `aud`. Refresh token opaco, guardado como hash, rotacionado a cada uso e entregue em cookie `__Host-`, `HttpOnly`, `Secure`, `SameSite=Lax`. A reutilização de um refresh token revoga toda a família. Permissões não ficam no JWT: são lidas com cache curto, para que uma revogação valha rapidamente.

**Consequências.** Sessões revogáveis e detecção de roubo de token. Exige consulta de sessão e permissões por requisição autenticada, mitigada por cache.

**Alternativas.** Sessão só em Redis: dependência crítica no cache. JWT longo: não é revogável.

## ADR-008: RabbitMQ com Outbox

**Contexto.** Os casos de uso são filas de trabalho: e-mail, webhooks, integrações e retentativas por mensagem.

**Decisão.** RabbitMQ com quorum queues e publisher confirms. Eventos são gravados no outbox na mesma transação do dado e publicados pelo Relay. Detalhes em [event-driven-architecture.md](event-driven-architecture.md).

**Consequências.** Nenhum evento se perde se o broker cair. A entrega é "ao menos uma vez", então todo consumidor precisa ser idempotente.

**Alternativas.** Kafka: indicado para replay e alto volume de streams, necessidades que ainda não existem. A publicação fica atrás de uma porta, permitindo reavaliar.

## ADR-009: Checkout em transação local, gateway fora dela

**Contexto.** Reserva de estoque, criação do pedido e evento precisam ser atômicos, mas uma chamada de rede dentro da transação prenderia conexões e travas.

**Decisão.** Reserva, pedido e outbox são gravados em **uma transação**, compartilhada entre módulos pelo contexto de tenant. O gateway é chamado depois do commit, com chave de idempotência. Se a chamada falhar, o pedido continua pendente e pode ser retomado. Reservas expiradas são liberadas por um job.

**Consequências.** Consistência forte e fluxo simples. É um acoplamento consciente entre `inventory` e `orders`: se forem extraídos, o fluxo vira uma saga usando as operações de reservar e liberar que já existem.

**Alternativas.** Saga desde o início: complexidade sem benefício enquanto tudo roda em um processo.

## ADR-010: Dinheiro em inteiros e identificadores UUIDv7

**Decisão.** Valores em `bigint` na menor unidade da moeda, sempre com código ISO 4217. Chaves primárias em UUIDv7, geradas pela aplicação.

**Consequências.** Sem erro de arredondamento binário. UUIDv7 é ordenável, fragmenta menos os índices e não revela volume de vendas como um número sequencial.

## ADR-011: Busca em PostgreSQL atrás de uma porta

**Decisão.** Interface `SearchEngine` no módulo `search`. Primeira implementação com `tsvector`, `pg_trgm` e `unaccent`. OpenSearch entra como segundo adaptador, alimentado por eventos, quando testes de carga ou requisitos de relevância justificarem.

**Consequências.** Menos infraestrutura no início. Recomendações e correção de termos avançada ficam limitadas até a troca.

## ADR-012: Dados de cartão nunca passam pela plataforma

**Decisão.** O número do cartão é digitado em campos hospedados pelo gateway, que devolve um token. A plataforma guarda apenas token, bandeira, últimos quatro dígitos e validade. A página de pagamento não carrega scripts de terceiros configurados pelo tenant.

**Consequências.** Reduz o escopo PCI DSS. O enquadramento final precisa ser confirmado com um avaliador qualificado.

## ADR-013: Temas por tokens validados

**Decisão.** Um tema é um documento JSON versionado com tokens (cores, fontes de uma lista permitida, raio, espaçamento, layouts e componentes de um registro). O backend valida formato e contraste. CSS ou JavaScript arbitrário de tenant não é aceito.

**Consequências.** Um tenant não consegue quebrar outro nem injetar código. Personalizações além do catálogo de tokens exigem evoluir o produto, não editar a loja do cliente.

## ADR-014: Segredos de tenant com envelope encryption

**Decisão.** Credenciais de gateways e dados pessoais sensíveis são cifrados com AES-256-GCM. Cada tenant tem sua chave de dados, protegida por uma chave mestra no KMS. O ID do tenant entra como dado autenticado. CPF e CNPJ têm um índice cego HMAC para busca exata.

**Consequências.** Um vazamento do banco não revela segredos, e a rotação é feita por tenant. Em desenvolvimento, a chave mestra vem de uma variável de ambiente local.
