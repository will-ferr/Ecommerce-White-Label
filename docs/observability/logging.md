# Logs

Logs estruturados em JSON, emitidos com `log/slog` na saída padrão e enviados ao Loki pelo OpenTelemetry Collector.

## Formato

```json
{
  "timestamp": "2026-11-18T14:03:22.481Z",
  "level": "INFO",
  "message": "order.created",
  "service": "api",
  "environment": "production",
  "version": "1.4.2",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "correlation_id": "0196f2c1-9b10-7a31-8d2c-4a6b9f0c1e22",
  "tenant_id": "0196f2c1-1111-7000-8000-000000000001",
  "module": "orders",
  "event": "order.created",
  "order_id": "0196f2c1-2222-7000-8000-000000000002"
}
```

## Categorias

| Categoria | Exemplos | Nível |
| --- | --- | --- |
| Requisição | Rota, status, duração | INFO |
| Erro | Falha inesperada, com causa encadeada | ERROR |
| Negócio | Pedido criado, pagamento confirmado | INFO |
| Administração | Alteração de permissão ou configuração, também gravada em `audit.entries` | INFO |
| Integração | Falha em gateway ou transportadora | WARN ou ERROR |
| Segurança | Login falho, tenant divergente, reuso de token | WARN |
| Pagamento | Mudança de estado, sem dados do meio de pagamento | INFO |

## Dados proibidos

Nunca registrar senhas, tokens, cookies, cabeçalho `Authorization`, chaves de API, número de cartão, CVV, CPF, CNPJ, e-mail, telefone, endereço, corpo de requisição ou corpo de webhook.

Proteções em camadas:

1. Tipos sensíveis em Go, como `Secret` e `Email`, implementam `slog.LogValuer` e sempre se apresentam mascarados.
2. Um handler de `slog` remove atributos com nomes proibidos.
3. O Collector aplica uma segunda redação antes do envio.
4. Um teste automatizado gera logs de fluxos sensíveis e falha se encontrar valores marcados.

## Rótulos no Loki

Apenas `service`, `environment` e `level` são rótulos. `tenant_id`, `trace_id` e os demais campos são consultados no conteúdo JSON, evitando alta cardinalidade.

## Retenção

| Tipo | Retenção inicial |
| --- | --- |
| Aplicação | 30 dias |
| Segurança | 180 dias |
| Auditoria (banco) | Definida por política legal e contratual |
