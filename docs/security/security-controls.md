# Controles de segurança

Referências: OWASP ASVS e OWASP Top 10. Este documento descreve controles técnicos planejados e **não constitui declaração de conformidade** com LGPD, PCI DSS ou qualquer norma.

## 1. Autenticação

| Tema | Controle |
| --- | --- |
| Senhas | Argon2id com os parâmetros do OWASP, mínimo de 10 caracteres e bloqueio de senhas comuns. A consulta a bases de senhas vazadas ainda não foi implementada |
| Realms | Plataforma, equipe da loja e comprador têm audiências e fluxos separados |
| Access token | JWT EdDSA, 10 minutos, mantido em memória no navegador |
| Refresh token | Opaco, hash no banco, rotativo, cookie `__Host-` `HttpOnly` `Secure` `SameSite=Lax` |
| MFA | TOTP e códigos de recuperação; obrigatório para Super Admin e configurável para a equipe da loja; passkeys em evolução futura |
| Sessões | Lista de dispositivos, revogação individual ou total, histórico de acessos |
| Recuperação de senha | Token de uso único, curto, guardado como hash; revoga sessões ao concluir |
| OAuth2 e OIDC | Ainda não implementado. Quando adotado, usará Authorization Code com PKCE |

## 2. Autorização

- RBAC com permissões granulares no formato `recurso:ação`, como `orders:refund`.
- Política deny-by-default: uma rota sem política declarada falha em um teste automatizado.
- Verificação em três níveis: tenant, permissão e propriedade do recurso.
- Planejado para a Fase 6: exigir MFA recente em ações críticas, como reembolso e troca de credenciais de gateway.
- Planejado para a Fase 7: acesso do suporte da plataforma a uma loja de forma temporária, justificada e auditada.

## 3. Proteção da aplicação

| Ameaça | Controle |
| --- | --- |
| SQL Injection | Consultas parametrizadas e geradas por sqlc |
| XSS | Escape padrão do Vue, sanitização de HTML no servidor, CSP com nonce |
| CSRF | Access token em cabeçalho; rota de refresh com `SameSite` e verificação de origem |
| SSRF | Cliente HTTP de saída único, com bloqueio de redes internas e metadados de nuvem |
| IDOR/BOLA | RLS, escopo de tenant e teste de autorização por endpoint |
| Abuso de API | Rate limit por IP, usuário, tenant e rota, com algoritmo no Redis |
| Brute force | Limite por conta e por IP, com atraso progressivo |
| Payload excessivo | Limite de corpo por rota, timeouts de leitura e escrita, limite de concorrência |
| Upload malicioso | URL pré-assinada, verificação do tipo real, reprocessamento de imagens, domínio separado |
| Erros | Formato RFC 9457 sem detalhes internos; `correlation_id` para suporte |

Cabeçalhos padrão: `Strict-Transport-Security`, `Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` e `frame-ancestors`.

CORS é fechado por padrão. O storefront usa a mesma origem da API, e os painéis têm uma lista explícita de origens.

## 4. Dados e criptografia

| Dado | Tratamento |
| --- | --- |
| Número do cartão e CVV | Nunca recebidos nem armazenados |
| Credenciais de gateway | Envelope encryption por tenant, usando KMS |
| CPF e CNPJ | Cifrados, com índice cego para busca exata |
| Senhas e tokens | Apenas hash |
| Tráfego | TLS 1.2 ou superior, inclusive entre serviços quando a rede não for confiável |
| Em repouso | Criptografia de disco e de backups |

## 5. Infraestrutura e cadeia de suprimentos

| Tema | Controle |
| --- | --- |
| Containers | Imagens mínimas, usuário sem root, sistema de arquivos somente leitura |
| Kubernetes | NetworkPolicy deny-by-default, Pod Security restricted, RBAC mínimo |
| Segredos | Fora do código, injetados por Secrets Manager, com rotação |
| CI | `gosec`, `govulncheck`, `npm audit`, Trivy, gitleaks, SBOM e dependências fixadas |
| Banco | Acesso privado, papéis mínimos, sem superusuário na aplicação |
| Testes dinâmicos | OWASP ZAP no pipeline de staging e pentest antes do lançamento |

## 6. Operação

- Auditoria somente inclusão para operações administrativas, com ator, tenant, ação, alvo, IP e `trace_id`.
- Alertas de falhas de login, reuso de refresh token, mudanças de credenciais e picos de negações.
- Plano de resposta a incidentes em `incident-response.md`, a ser criado na Fase 9.
- Prazos de correção de vulnerabilidades por severidade, definidos na Fase 8.

## 7. LGPD

| Requisito | Controle técnico | Fase |
| --- | --- | --- |
| Consentimento | Registro com finalidade, versão do texto, data e origem | 7 |
| Direitos do titular | Fluxo de solicitação com prazo, status e evidências | 7 |
| Exportação | Geração de arquivo com os dados pessoais do titular | 7 |
| Exclusão | Remoção ou anonimização, preservando o que a lei exige manter | 7 |
| Minimização | Eventos e logs sem dados pessoais desnecessários | 2 |
| Controle de acesso | RBAC e auditoria de acesso a dados pessoais | 4 |
| Retenção | Política configurável por tipo de dado | 7 |

**Divisão de responsabilidades.** Em geral, cada loja atua como controladora dos dados de seus clientes, e a plataforma como operadora. Para dados da própria operação do SaaS, a plataforma é controladora. Contratos, bases legais, política de privacidade, encarregado e comunicação de incidentes são responsabilidades operacionais e jurídicas que o software não resolve sozinho. Esse enquadramento precisa de validação jurídica.
