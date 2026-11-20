# Runbooks

Cada alerta tem um runbook. Os alertas e seus limites estão em `deployments/observability/alerts/ecommerce.rules.yaml`; os dashboards, no Grafana, pasta **E-commerce**.

| Runbook | Alertas |
| --- | --- |
| [Orçamento de erros da API](api-error-budget.md) | `ApiErrorBudgetFastBurn` (crítico) e `ApiErrorBudgetSlowBurn` (aviso) |
| [Latência alta da API](api-latency.md) | `ApiHighLatency` (aviso) |
| [Serviço sem telemetria](service-down.md) | `ServiceTelemetryMissing` (crítico) |
| [Outbox atrasado](outbox-lag.md) | `OutboxLagging` (crítico) |
| [Eventos na fila de mensagens mortas](dead-letters.md) | `EventsDeadLettered` (crítico) e `EventsRetryingOften` (aviso) |
| [Falhas de entrega de e-mail](email-delivery.md) | `EmailDeliveryFailing` (aviso) |
| [Notificações de pagamento](payment-notifications.md) | `PaymentAmountMismatch` (crítico) e `PaymentNotificationsRejected` (aviso) |
| [Pool do banco saturado ou erros de consulta](database-pool.md) | `DatabasePoolSaturated` (crítico) e `DatabaseErrors` (aviso) |
| [Eventos de segurança](security-events.md) | `TenantMismatch` (crítico), `LoginAttack`, `RefreshTokenReuse` e `RateLimitSpike` (avisos) |
| [Backup e restauração](backup-restore.md) | Procedimento, sem alerta associado |
