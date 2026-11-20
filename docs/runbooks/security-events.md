# Eventos de segurança

**Alertas:** `TenantMismatch` (crítico), `LoginAttack`, `RefreshTokenReuse` e `RateLimitSpike` (avisos)

**O que significa.** Token de uma loja usado em outra, pico de senhas erradas, refresh token reutilizado ou muitas requisições bloqueadas por limite.

**Impacto.** Pode ser ataque em andamento. O token entre lojas é sempre recusado, mas indica uma tentativa ou um defeito de isolamento.

**Dashboard.** Segurança

## Antes de agir

1. Abra o dashboard indicado e confirme o horário em que o problema começou.
2. Procure deploys, migrações ou mudanças de configuração nesse horário.
3. Registre no canal do incidente o que observou e o que vai tentar.

## Diagnóstico

1. Logs dos eventos: `{service_name="ecommerce-api"} |= "security."` no Loki.
2. Auditoria da loja ou da plataforma no admin e no console (Auditoria).
3. Origem do tráfego nos logs do edge.

## Ação

- `TenantMismatch`: trate como incidente de segurança. Preserve logs e traces, identifique a conta e revogue as sessões dela.
- `LoginAttack`: os limites por conta e por IP já atrasam as tentativas. Se vier de poucas origens, bloqueie no edge.
- `RefreshTokenReuse`: a sessão envolvida já foi encerrada automaticamente. Avise o titular da conta se o padrão se repetir.
- `RateLimitSpike`: confirme se é abuso ou um cliente legítimo mal configurado antes de ajustar limites.

## Depois

- Registre causa, impacto e duração no histórico de incidentes.
- Se o alerta disparou sem impacto real, ajuste o limite com base nos dados, por pull request.
