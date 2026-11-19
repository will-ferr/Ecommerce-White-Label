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
| 015 | Gestão de lojas por linha de comando até existir o console | Aceito |
| 016 | Operadores da plataforma em um tenant reservado | Aceito |

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

## ADR-015: Gestão de lojas por linha de comando até existir o console

**Contexto.** A Fase 3 cria lojas e temas antes de existir um console autenticado para gerenciá-los. Uma rota HTTP de escrita sem autorização exporia todas as lojas.

**Decisão.** O cadastro é feito pela ferramenta `tenantctl`, com o papel de banco `app_platform`. A API usa apenas leitura nas tabelas de lojas.

**Consequências.** Nenhuma superfície de escrita fica exposta na internet. As regras ficam nos casos de uso do módulo e serão reaproveitadas pelo console da Fase 7. Desde a Fase 4, cada alteração feita pela ferramenta é registrada na trilha de auditoria, com o ator `tenantctl`.

**Alternativas.** Rotas protegidas por uma chave fixa: segredo compartilhado, sem identidade do operador e difícil de revogar.

## ADR-016: Operadores da plataforma em um tenant reservado

**Contexto.** Compradores, equipes de loja e operadores da plataforma seguem os mesmos fluxos de senha, sessão e MFA, mas não podem compartilhar contas nem acessos.

**Decisão.** Todas as contas ficam em `identity.users`, separadas pelo campo `realm`. Operadores pertencem a um tenant reservado, sem domínio e fora da resolução de lojas. Cada portal tem rotas, audiência de token e cookies próprios.

**Consequências.** O RLS isola operadores das lojas sem código duplicado. Um token só é aceito no portal para o qual foi emitido. A leitura global entre lojas continua restrita ao papel `app_platform` e será exposta no console da Fase 7.

**Alternativas.** Tabelas separadas para operadores: duplicariam sessões, tokens, MFA e consultas sem ganho de isolamento.

## ADR-017: Núcleo comercial em três módulos e pagamento de teste explícito

**Contexto.** A Fase 5 entrega catálogo, estoque, carrinho, checkout, pedidos e endereços. O modelo da Fase 1 previa um módulo por contexto e tabelas de listas de preço, marcas e depósitos múltiplos. Ainda não existe gateway real, que chega na Fase 6.

**Decisão.**

- Três módulos Go: `catalog` (produtos, variantes, categorias, preços), `inventory` (saldos, reservas, movimentações) e `sales` (carrinho, checkout, pedidos, pagamentos, frete e endereços). Cada contexto de `sales` tem schema próprio no banco (`cart`, `checkout`, `orders`, `payments`, `shipping`, `customers`), o que permite separar o módulo depois sem migrar dados.
- O preço fica na variante, com histórico em `catalog.price_history`. Listas de preço, preços por quantidade e por grupo entram quando houver promoções na Fase 7.
- Um depósito padrão por loja. A reserva é um `UPDATE` condicional sob `CHECK (reserved <= on_hand)`, com as variantes travadas em ordem fixa para evitar deadlock. Reserva, pedido, pagamento e chave de idempotência são gravados na mesma transação.
- O checkout recalcula preços, estoque e frete no servidor. Se algo mudou desde a última revisão, devolve `PRICE_CHANGED` ou `OUT_OF_STOCK` com o novo resumo, sem criar o pedido.
- Pagamentos passam por uma interface `Provider`. A única implementação é o **Pagamento de teste**, exibido com esse nome ao comprador e à equipe, habilitado por `PAYMENTS_TEST_PROVIDER` e recusado pela configuração de produção.
- Pedidos de visitantes são consultados por um token HMAC derivado de `AUTH_SIGNING_KEY`, sem senha. Pedidos não pagos expiram por uma rotina com advisory lock, para rodar em uma instância por vez.
- Os tipos de comércio do cliente TypeScript são escritos à mão em `commerce.ts` e verificados pelos testes E2E. O OpenAPI documenta as rotas, mas o gerador cobre só identidade e loja.

**Consequências.** Uma compra completa funciona de ponta a ponta sem simular um gateway real. A troca pelo gateway da Fase 6 implementa `Provider` e passa o estorno para o fluxo assíncrono. Impostos ficam zerados e identificados como "não calculados" até existir configuração fiscal validada.

**Alternativas.** Um módulo por contexto já na Fase 5: mais fronteiras e transações distribuídas antes de qualquer necessidade de implantação separada. Simular respostas de um gateway real: violaria a regra de não apresentar dados simulados como funcionalidade real.

## ADR-018: Integrações assíncronas com o provedor de pagamento em aberto

**Contexto.** A Fase 6 pede mensageria, webhooks, frete, e-mail durável, busca e um gateway real em sandbox. A escolha do gateway foi deixada em aberto, e uma integração só pode ser declarada funcional depois de testada no sandbox do provedor.

**Decisão.**

- **Worker.** Um processo `cmd/worker`, com o mesmo código da API, executa o relay do outbox, os consumidores, o envio de e-mail, a expiração de pedidos e a limpeza das tabelas de mensageria. O relay roda no próprio worker, e não em um `cmd/relay` separado: o `SKIP LOCKED` já permite várias instâncias, e um processo a menos simplifica a operação.
- **Outbox e inbox.** Eventos são gravados em `messaging.outbox` na transação do negócio. O relay publica no exchange `domain.events` com publisher confirms e só então marca o evento como publicado. Cada consumidor registra o evento em `messaging.inbox` na mesma transação do efeito. O papel da aplicação só insere no outbox. A leitura entre lojas passa por funções `SECURITY DEFINER` que expõem apenas o necessário.
- **Retentativas.** Cada consumidor tem uma fila quorum, filas de espera com TTL (10 s, 1 min, 10 min e 1 h) e uma DLQ clássica. Erros marcados como permanentes vão direto para a DLQ. A DLQ é inspecionada e reprocessada pelo comando `worker dlq`, que exige motivo e registra a operação na auditoria da plataforma.
- **E-mail durável.** Mensagens ficam em `notifications.emails`, cifradas com AES-GCM (chave derivada de `MFA_ENCRYPTION_KEY`, com loja e ID como dado autenticado). O conteúdo é apagado após a entrega. O dispatcher aluga lotes e tenta até 6 vezes. Endereços são removidos das mensagens de erro antes de irem para logs e banco.
- **Pagamentos.** A interface `Provider` passa a incluir `ParseNotification`. Um pedido só é marcado como pago por uma notificação autenticada, nunca pela resposta da chamada de cobrança. A notificação é recebida em `POST /api/v1/webhooks/payments/{provider}` no domínio da loja e gravada em `payments.webhook_events`, com chave única por provedor e ID do evento. Uma repetição não tem efeito. O valor precisa coincidir com o do pagamento. Uma captura que chega depois do cancelamento gera estorno.
- **Estorno.** O estorno é assíncrono: o cancelamento marca o pagamento como `refund_pending` e publica `payments.refund.requested.v1`. O consumidor chama o provedor com o ID do pagamento como chave de idempotência.
- **Provedor de teste.** O provedor de teste assina suas notificações com HMAC (chave derivada de `AUTH_SIGNING_KEY`, com validade de 5 minutos) e as entrega pelo mesmo fluxo de webhook. Sem provedor configurado, o checkout responde `PAYMENT_UNAVAILABLE`, em vez de criar pedidos que nunca poderiam ser pagos.
- **Frete.** O frete por tabela usa faixas de CEP e peso definidas pela própria loja. A cotação com transportadoras fica para quando houver um provedor escolhido.
- **Busca.** A busca usa PostgreSQL (ADR-011): `tsvector` em português sem acentos, com pesos para nome, marca e descrição, e `pg_trgm` para erros de digitação. O documento de busca é um índice de expressão, e não uma coluna, para que as listagens não o carreguem.

**Consequências.** Integrar um gateway real significa implementar `Provider` (cobrança, estorno e verificação de assinatura), guardar as credenciais da loja com envelope encryption (ADR-014) e validar o fluxo no sandbox do provedor. O critério "fluxo validado no sandbox real" da Fase 6 continua pendente até essa escolha. Notificações aos compradores são exatamente uma por evento no banco. O envio por SMTP é ao menos uma vez, porque um servidor pode aceitar a mensagem e cair antes de responder.

**Alternativas.** Publicar no broker dentro da transação: perde mensagens quando o commit falha. Fila de e-mail no RabbitMQ: colocaria links de acesso no broker sem cifragem e duplicaria o controle de tentativas. Implementar um gateway sem acesso ao sandbox: seria uma integração declarada sem teste real, o que as regras do projeto proíbem.
