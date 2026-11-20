# Orçamento de erros da API

**Alertas:** `ApiErrorBudgetFastBurn` (crítico) e `ApiErrorBudgetSlowBurn` (aviso)

**O que significa.** A proporção de respostas 5xx está acima do que o SLO de 99,9% permite. O alerta rápido indica que, se nada mudar, o orçamento do mês acaba em cerca de dois dias; o lento, em cerca de cinco.

**Impacto.** Compradores e equipes recebem erros. Checkout e login podem estar afetados.

**Dashboard.** API → "Respostas por status" e "Requisições por rota"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Identifique as rotas com erro: no Prometheus, `sum by (http_route) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[5m]))`.
2. Abra os logs com erro no Grafana, fonte Loki: `{service_name="ecommerce-api"} | json | level="ERROR"`. Use o `trace_id` para ver o trace no Tempo.
3. Verifique dependências no health check: `docker compose exec api /app/api -healthcheck` e `curl http://localhost:8080/api/v1/status`.
4. Se os erros são de banco, siga também [Pool do banco](database-pool.md).

## Ação

- Se começou após um deploy, faça rollback para a versão anterior.
- Se uma dependência caiu (PostgreSQL, Redis), restabeleça-a; a API volta sozinha.
- Se uma única rota falha por dado inválido, corrija o dado com o fluxo da aplicação, nunca editando o banco à mão.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
