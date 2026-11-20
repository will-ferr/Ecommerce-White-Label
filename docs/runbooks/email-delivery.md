# Falhas de entrega de e-mail

**Alertas:** `EmailDeliveryFailing` (aviso)

**O que significa.** O servidor SMTP recusou ou não respondeu a várias mensagens em 15 minutos.

**Impacto.** Confirmações de pedido, convites e redefinições de senha chegam atrasados. Cada mensagem tenta 6 vezes em cerca de 7 horas antes de desistir.

**Dashboard.** Negócio e mensageria → "E-mails por tipo e resultado"

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Leia o erro (sem endereços, que são removidos): `docker compose logs worker | grep mail.send_failed`.
2. Teste se o servidor responde a partir da rede dos serviços: `docker run --rm --network ecommerce-white-label_default busybox:1.37 nc -zv mailpit 1025` (troque pelo servidor configurado em `SMTP_ADDR`).

## Ação

- Credencial ou TLS errados: corrija `SMTP_*` e reinicie o worker. As mensagens pendentes são enviadas na próxima tentativa.
- Limite do provedor de e-mail: reduza o envio ou aumente a cota.
- Mensagens que esgotaram as tentativas ficam com status `failed` e sem conteúdo. Elas não podem ser reenviadas; oriente o cliente a repetir a ação (por exemplo, pedir nova redefinição de senha).

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
