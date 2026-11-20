# Pool do banco saturado ou erros de consulta

**Alertas:** `DatabasePoolSaturated` (crítico) e `DatabaseErrors` (aviso)

**O que significa.** Mais de 85% das conexões do pool estão em uso, ou consultas estão falhando.

**Impacto.** Requisições esperam por conexão e passam a falhar por tempo esgotado.

**Dashboard.** Banco e processos → "Uso do pool de conexões", "Esperas por conexão" e "Erros de consulta"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Veja as consultas ativas: `SELECT pid, state, wait_event, now() - query_start AS duration, left(query, 80) FROM pg_stat_activity WHERE datname = 'ecommerce' ORDER BY duration DESC;`.
2. Procure bloqueios: `SELECT * FROM pg_locks WHERE NOT granted;`.
3. Confira se o tráfego aumentou em relação à [capacidade medida](../observability/capacity.md).

## Ação

- Uma consulta presa: o `statement_timeout` de 10 s do papel da API a encerra; se for outra origem, use `pg_cancel_backend(pid)`.
- Tráfego acima da capacidade: adicione réplicas da API e, se necessário, aumente `DB_MAX_CONNS` dentro do limite do servidor e do PgBouncer.
- Erros por permissão ou RLS indicam defeito: abra um incidente e não conceda privilégios extras ao papel da API.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
