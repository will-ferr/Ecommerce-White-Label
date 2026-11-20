# Latência alta da API

**Alertas:** `ApiHighLatency` (aviso)

**O que significa.** O p95 das respostas da API passou de 300 ms por 10 minutos, com tráfego relevante.

**Impacto.** Lojas lentas reduzem conversão. Timeouts podem virar erros 5xx.

**Dashboard.** API → "Latência p95 por rota"; Banco e processos → "Latência p95 das consultas" e "Esperas por conexão"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Veja quais rotas ficaram lentas e se a lentidão está nas consultas ao banco.
2. Abra traces lentos no Tempo: `{ resource.service.name = "ecommerce-api" && duration > 300ms }`.
3. Compare o tráfego atual com a [capacidade medida](../observability/capacity.md).

## Ação

- Se o pool de conexões está saturado, siga [Pool do banco](database-pool.md).
- Se o tráfego está acima da capacidade, adicione réplicas da API.
- Se uma consulta específica piorou, verifique o plano com `EXPLAIN ANALYZE` em uma réplica e crie o índice por migration.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
