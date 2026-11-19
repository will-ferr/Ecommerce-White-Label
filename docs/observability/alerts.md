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

Métricas já emitidas para estes alertas: `messaging_outbox_lag_seconds`, `messaging_events_consumed_total` (por consumidor e resultado, incluindo `dead`) e `mail_messages_total` (por tipo e resultado). As regras do Prometheus entram na Fase 8.

Alertas não incluem dados pessoais e trazem links para o dashboard e para o runbook correspondente.
