# Capacidade medida

Medição feita na Fase 8 com k6 (`scripts/load-test.sh`), em 20/11/2026. Os números são a **linha de base do ambiente local** e não substituem a medição em produção, que entra na Fase 9.

## Ambiente

- Uma máquina com Docker Desktop (WSL 2), 16 vCPUs e 8 GB de memória para os containers.
- Todos os serviços rodaram na mesma máquina, ao mesmo tempo: edge, uma instância da API, worker, PostgreSQL, Redis, RabbitMQ, coletor OpenTelemetry, Prometheus, Loki, Tempo, Grafana e o próprio k6.
- API com pool de 20 conexões (`DB_MAX_CONNS`). Limites de taxa ligados com os valores padrão.

## Carga

- **Navegação:** cada iteração lê a configuração da loja, a listagem e um produto. Em 30% das iterações, faz também uma busca. A taxa sobe em quatro degraus de 1 minuto até o pico e se mantém no pico por 2 minutos.
- **Compras:** em paralelo, um ritmo constante de carrinho, resumo e pedido, com frete calculado por CEP.
- **Visitantes e lojas:** cada iteração usa um IP diferente da faixa reservada a testes (198.18.0.0/15). A carga se distribui entre 16 lojas, para que os limites por IP (600/min) e por loja (6.000/min) se comportem como em produção.

## Resultados

| Pico de navegação | Requisições/s na API | p95 da navegação | p95 do checkout | Erros | Pedidos/s |
| --- | --- | --- | --- | --- | --- |
| 150 iterações/s | até ~500 | 9 ms | 55 ms | 0 erros 5xx; 0,05% de 429 | 3 (configurado) |
| 250 iterações/s | até ~840 | 541 ms no teste todo; 10 ms em regime | 3 s | 0 erros 5xx; 1,7% de falhas no checkout durante o pico de latência | 3,7 |
| 400 iterações/s | até ~1.350 | 0,1 a 2 s | acima de 2 s | 0 erros 5xx; iterações descartadas pelo k6 | 1,8 |

Na rodada de 250, o p95 por minuto ficou em 8 a 17 ms até 565 req/s. Ele subiu para 0,7 a 2,3 s entre 600 e 660 req/s e voltou a 10 ms com 840 req/s estáveis.

## Conclusões

- **Capacidade com SLO:** com p95 abaixo de 300 ms e menos de 0,1% de erros, uma instância da API atende **cerca de 500 requisições por segundo** de forma contínua, com folga.
- **Saturação:** começa entre 600 e 1.300 requisições por segundo e aparece primeiro em picos transitórios de latência.
- **Gargalo:** é o **pool de conexões do banco**, não a CPU. Nos traces das requisições lentas, o tempo está em `pool.acquire` (até 2,2 s), enquanto as consultas levam menos de 35 ms. As esperas por conexão chegaram a 1.000 por segundo. No pico, a API usou de 3 a 5 núcleos, e o PostgreSQL de 3 a 4.
- **Checkout:** com 3 a 5 pedidos por segundo junto da navegação, o p95 fica em 55 ms enquanto o sistema não está saturado.
- **Limites intencionais:** cada loja é limitada a 6.000 requisições por minuto (100/s) e cada IP a 600 por minuto. Uma única loja com tráfego acima disso recebe 429 por desenho, não por falta de capacidade.

## Defeito encontrado e corrigido

Na primeira medição, 5,5% das respostas eram 502, gerados pelo edge. O nginx abria uma conexão nova para cada requisição. Sob carga, as portas efêmeras se esgotaram, com o erro `Address not available`. O edge passou a usar `upstream` com `keepalive` e resolução dinâmica (`resolve`). Com a mesma carga, os 5xx foram a zero e o p95 caiu de 170 ms para 9 ms.

## Recomendações para a Fase 9

1. Dimensionar `DB_MAX_CONNS` por réplica, junto com o PgBouncer, e manter conexões mínimas aquecidas (`DB_MIN_CONNS`), para evitar a abertura de conexões nos picos.
2. Escalar a API horizontalmente a partir de ~400 req/s por réplica, deixando margem até a saturação medida.
3. Repetir a medição no cluster de produção, com os serviços de observabilidade fora das máquinas da aplicação.
4. Algumas requisições isoladas de vários minutos apareceram no k6 na rodada de 400. A causa não foi identificada, e elas não entram no p95.

## Como repetir

```bash
bash scripts/load-test.sh 150 3   # pico de navegação e pedidos por segundo
```

O script prepara as lojas de carga e grava o resumo em `tests/load/results/`. Acompanhe pelo dashboard **API** no Grafana.
