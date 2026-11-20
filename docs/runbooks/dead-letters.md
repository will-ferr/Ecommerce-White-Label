# Eventos na fila de mensagens mortas

**Alertas:** `EventsDeadLettered` (crítico) e `EventsRetryingOften` (aviso)

**O que significa.** Um consumidor falhou em todas as tentativas (10 s, 1 min, 10 min e 1 h) ou recebeu um evento que nunca poderá processar.

**Impacto.** Cada evento morto é uma ação que não aconteceu, como um e-mail ao comprador ou um estorno.

**Dashboard.** Negócio e mensageria → "Eventos consumidos por resultado"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Liste as mensagens, com tipo, loja, tentativas e erro: `docker compose exec worker /app/worker dlq list --consumer sales-notifications`.
2. Leia o erro dos logs: `docker compose logs worker | grep messaging.event_dead`.

## Ação

- Corrija a causa (configuração, dado ou defeito) antes de reprocessar.
- Reprocesse com motivo, que fica na auditoria: `docker compose exec worker /app/worker dlq replay --consumer sales-notifications --limit 50 --reason "SMTP corrigido"`.
- Para estornos (`sales-refunds`), confira no provedor se o valor já foi devolvido antes de reprocessar. A chave de idempotência evita estorno duplo nos provedores que a aceitam.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
