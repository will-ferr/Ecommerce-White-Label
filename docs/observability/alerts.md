# Alertas

Alertas indicam impacto ao usuário ou risco iminente, e cada um terá um runbook. Os limites abaixo são iniciais e serão calibrados com dados reais.

| Severidade | Resposta |
| --- | --- |
| `critical` | Aciona o plantão imediatamente |
| `warning` | Abre uma notificação para análise no horário comercial |

## Regras iniciais

| Alerta | Condição | Severidade |
| --- | --- | --- |
| Consumo rápido do orçamento de erro | Taxa de 14,4x o orçamento em 1 h e em 5 min | critical |
| Consumo lento do orçamento de erro | Taxa de 6x o orçamento em 6 h e em 30 min | warning |
| Latência alta | p95 da API acima do SLO por 10 min | warning |
| Indisponibilidade | Probe externo falha em 2 regiões por 2 min | critical |
| Falha de pagamentos | Mais de 20% de falhas técnicas por provedor em 10 min | critical |
| Webhooks rejeitados | Pico de assinaturas inválidas | warning |
| Erro de integração | Mais de 10% de erros por integração em 15 min | warning |
| Banco saturado | Pool acima de 85% por 5 min | critical |
| Replicação atrasada | Atraso da réplica acima de 60 s por 5 min | warning |
| Outbox atrasado | Evento mais antigo com mais de 2 min | critical |
| Fila crescendo | Crescimento contínuo por 15 min | warning |
| DLQ com mensagens | Qualquer mensagem nova em fila de pagamentos ou pedidos | critical |
| Pedidos parados | Pedido pago sem processamento além do limite | critical |
| Queda de pedidos | Volume muito abaixo do padrão histórico da mesma hora | warning |
| Infraestrutura | Reinícios em sequência, disco acima de 85%, memória perto do limite | warning |
| Divergência de tenant | Aumento de `security.tenant_mismatch` | critical |
| Ataque de login | Pico de falhas de autenticação | warning |
| Reuso de refresh token | Crescimento acima do padrão | warning |
| Certificado | Expira em menos de 14 dias | warning |
| Backup | Último backup ou teste de restauração fora do prazo | critical |

Os alertas de consumo de orçamento seguem o método de múltiplas janelas, que reduz alarmes falsos e detecta tanto falhas rápidas quanto degradações lentas.

## Implementado na Fase 8

As regras estão em `deployments/observability/alerts/ecommerce.rules.yaml` e têm testes em `ecommerce.rules.test.yaml`, executados pelo CI com `promtool test rules`. Cada alerta aponta para um [runbook](../runbooks/README.md).

No ambiente local, o Alertmanager entrega as notificações no Mailpit (<http://localhost:8025>), para `plantao@plataforma.localhost`. Alertas críticos são agrupados em 10 segundos e repetidos a cada hora; avisos, a cada 4 horas.

| Alerta | Condição implementada | Severidade |
| --- | --- | --- |
| `ApiErrorBudgetFastBurn` | Erros 5xx acima de 14,4 × 0,1% em 1 h e em 5 min, por 2 min | critical |
| `ApiErrorBudgetSlowBurn` | Erros 5xx acima de 6 × 0,1% em 6 h e em 30 min, por 15 min | warning |
| `ApiHighLatency` | p95 acima de 300 ms por 10 min, com tráfego | warning |
| `ServiceTelemetryMissing` | API, worker ou plataforma sem métricas por 3 min | critical |
| `OutboxLagging` | Evento mais antigo sem publicar há mais de 2 min | critical |
| `EventsDeadLettered` | Qualquer evento novo na DLQ em 10 min | critical |
| `EventsRetryingOften` | Mais de 20 novas tentativas por consumidor em 15 min | warning |
| `EmailDeliveryFailing` | Mais de 5 falhas de envio em 15 min | warning |
| `PaymentAmountMismatch` | Qualquer pagamento confirmado com valor diferente | critical |
| `PaymentNotificationsRejected` | Mais de 10 notificações com assinatura inválida em 10 min | warning |
| `DatabasePoolSaturated` | Pool acima de 85% por 5 min | critical |
| `DatabaseErrors` | Mais de 1 erro de consulta por segundo por 5 min | warning |
| `TenantMismatch` | Qualquer token de uma loja usado em outra | critical |
| `LoginAttack` | Mais de 50 logins com senha errada em 5 min | warning |
| `RefreshTokenReuse` | Mais de 5 reusos de token de sessão em 15 min | warning |
| `RateLimitSpike` | Mais de 200 requisições bloqueadas em 5 min | warning |

Os alertas de replicação, certificado, backup, queda de pedidos e pedidos parados dependem da infraestrutura de produção e entram na Fase 9.

Alertas não incluem dados pessoais e trazem links para o dashboard e para o runbook correspondente.
