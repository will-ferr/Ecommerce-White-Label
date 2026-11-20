# Outbox atrasado

**Alertas:** `OutboxLagging` (crítico)

**O que significa.** O evento mais antigo ainda não publicado tem mais de 2 minutos. O relay do worker não está publicando no RabbitMQ.

**Impacto.** E-mails de pedido e estornos atrasam. Nenhum evento se perde: eles ficam no banco até serem publicados.

**Dashboard.** Negócio e mensageria → "Atraso do outbox"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Confirme que o worker está de pé: `docker compose exec worker /app/worker -healthcheck`.
2. Procure falhas do relay: `docker compose logs --tail=100 worker | grep messaging.relay_failed`.
3. Verifique o RabbitMQ: `docker compose exec rabbitmq rabbitmq-diagnostics -q status`.

## Ação

- Se o RabbitMQ caiu ou recusou a conexão, restabeleça-o. O relay tenta de novo sozinho, com espera de até 30 segundos.
- Se o worker está parado, siga [Serviço sem telemetria](service-down.md).
- Não marque eventos como publicados à mão: o relay garante a publicação ao menos uma vez.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
