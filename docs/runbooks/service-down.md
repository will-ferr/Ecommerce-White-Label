# Serviço sem telemetria

**Alertas:** `ServiceTelemetryMissing` (crítico)

**O que significa.** A API, o worker ou o serviço da plataforma deixou de enviar métricas por 3 minutos.

**Impacto.** Se a API caiu, as lojas estão fora do ar. Se o worker caiu, e-mails, estornos e expiração de pedidos param. Se o serviço da plataforma caiu, o console não funciona.

**Dashboard.** Banco e processos → "Goroutines" (o serviço some do gráfico)

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Veja o estado dos containers: `docker compose ps api worker platform otel-collector`.
2. Leia os últimos logs: `docker compose logs --tail=100 worker`.
3. Se os serviços estão de pé, o problema é a coleta: `docker compose logs --tail=50 otel-collector`.

## Ação

- Reinicie o serviço parado: `docker compose up -d worker`. Ele recusa iniciar com configuração inválida e lista o motivo no log.
- Se o coletor caiu, reinicie-o; os serviços continuam atendendo enquanto isso.
- Depois de restabelecer o worker, confirme que o [outbox](outbox-lag.md) voltou a esvaziar.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
