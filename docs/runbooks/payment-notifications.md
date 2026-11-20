# Notificações de pagamento

**Alertas:** `PaymentAmountMismatch` (crítico) e `PaymentNotificationsRejected` (aviso)

**O que significa.** Valor divergente: o provedor confirmou um pagamento com valor diferente do pedido, e o pedido não foi marcado como pago. Notificações rejeitadas: chegaram notificações com assinatura inválida ou vencida.

**Impacto.** Pedidos pagos podem ficar aguardando pagamento e expirar. Rejeições em massa podem ser uma tentativa de forjar pagamentos, que é bloqueada.

**Dashboard.** Negócio e mensageria → "Notificações de pagamento por resultado"; Segurança → "Eventos de segurança"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Consulte as notificações e seus resultados: `SELECT provider, status, outcome, received_at FROM payments.webhook_events ORDER BY received_at DESC LIMIT 20;` em uma réplica de leitura, com acesso auditado.
2. Para rejeições, veja a origem das requisições nos logs do edge e se a chave de webhook do provedor foi trocada.

## Ação

- Valor divergente: confirme o valor no painel do provedor. Se o cliente pagou a mais ou a menos, trate com o cliente e estorne pelo provedor; a notificação de estorno cancela o pedido.
- Rejeições por chave errada: atualize a chave do webhook. O provedor reenvia as notificações.
- Rejeições por ataque: bloqueie a origem no edge. Nenhum pedido foi alterado.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
