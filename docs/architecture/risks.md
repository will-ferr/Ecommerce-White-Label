# Riscos técnicos e arquiteturais

Probabilidade (P) e impacto (I) em escala Baixo, Médio e Alto, ordenados por prioridade.

| # | Risco | P | I | Mitigação | Fase |
| --- | --- | --- | --- | --- | --- |
| R1 | **Vazamento entre tenants** por consulta, cache ou job sem contexto | Média | Alto | Cinco camadas de isolamento, RLS forçado e testes obrigatórios no CI | 3 |
| R2 | **Escopo muito maior que a capacidade de entrega** | Alta | Alto | MVP definido no roadmap, integrações por adaptador, um provedor de cada tipo primeiro | Todas |
| R3 | **Falha única derruba todas as lojas**, consequência do backend compartilhado | Média | Alto | Réplicas em múltiplas zonas, deploy gradual, rollback, limites por tenant | 8, 9 |
| R4 | **Venda além do estoque ou cobrança duplicada** | Média | Alto | Reserva atômica, idempotência, conciliação e testes de concorrência | 5, 6 |
| R5 | **Escopo PCI DSS ampliado** por scripts na página de pagamento | Média | Alto | Campos hospedados, página de pagamento sem scripts de terceiros, validação com avaliador qualificado | 6 |
| R6 | **Regras fiscais incorretas**, agravadas pela transição da reforma tributária (IBS e CBS) | Alta | Alto | Regras configuráveis, provedor especializado, validação contábil, nenhuma alíquota no código | 7 |
| R7 | **Vizinho barulhento** degrada outras lojas | Alta | Médio | Rate limit, cotas, timeouts e consumo justo nas filas | 4, 8 |
| R8 | **Emissão de TLS para domínios próprios** em escala | Média | Alto | Avaliar serviço de domínios do provedor de borda ou TLS sob demanda antes da Fase 9 | 3, 9 |
| R9 | **Fronteiras entre módulos se degradam** com o tempo | Média | Médio | `internal/` por módulo, proibição de `JOIN` entre schemas, verificação no CI | 2 |
| R10 | **RLS afeta o desempenho** em tabelas grandes | Média | Médio | Índices iniciados por `tenant_id`, política com `(SELECT ...)`, medição com k6 | 5, 8 |
| R11 | **Mudanças em APIs de terceiros** e dependência de contas contratadas | Alta | Médio | Adaptadores isolados, contract tests, circuit breakers | 6 |
| R12 | **Migrations travam a produção** em um banco compartilhado | Média | Alto | Padrão expandir e contrair, `lock_timeout`, índices `CONCURRENTLY` | 5, 9 |
| R13 | **Cache de SSR serve conteúdo errado** entre lojas | Baixa | Alto | Host na chave, nada autenticado em cache compartilhado, testes específicos | 3 |
| R14 | **Comprometimento de conta de Super Admin** | Baixa | Alto | MFA forte, origem separada, acesso temporário, auditoria e alertas | 4 |
| R15 | **Cardinalidade de telemetria** gera custo e instabilidade | Média | Médio | `tenant_tier` nos rótulos, limites no Collector | 2 |
| R16 | **Restauração de um único tenant** é complexa no banco compartilhado | Média | Médio | Procedimento documentado e ensaiado | 9 |
| R17 | **Responsabilidades LGPD indefinidas** entre plataforma e lojas | Média | Alto | Controles técnicos, contrato e revisão jurídica antes da comercialização | 7, 9 |
| R18 | **Custo operacional da stack** para uma equipe pequena | Alta | Médio | Serviços gerenciados em produção, automação, Compose com perfis no desenvolvimento | 2, 9 |

## Dependências externas

Estes itens não podem ser resolvidos apenas com código:

- Contas e credenciais de sandbox dos gateways e transportadoras.
- Contrato com os Correios, necessário para algumas APIs.
- Provedor de nuvem, KMS e serviço de e-mail.
- Validação contábil, jurídica e de segurança antes do lançamento.
