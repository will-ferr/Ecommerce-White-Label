# Backup e restauração

**Situação:** procedimento ensaiado, sem alerta associado. O alerta de backup atrasado entra na Fase 9, junto com o armazenamento externo dos backups.

## Ensaio automatizado

```bash
bash scripts/backup-restore-test.sh
```

O CI executa o ensaio a cada mudança. Ele:

1. Gera um backup lógico do banco em uso (`pg_dump -Fc`).
2. Sobe um PostgreSQL novo e cria os papéis da aplicação com o mesmo script do ambiente.
3. Restaura em uma única transação. Qualquer erro aborta tudo.
4. Verifica a cópia:
   - as contagens de lojas, contas, produtos, pedidos, pagamentos, auditoria e outbox são iguais às da origem, assim como a versão das migrations;
   - todas as tabelas com `tenant_id` mantêm o RLS forçado;
   - o papel da aplicação, sem loja definida, não lê nenhum pedido;
   - nenhum papel da aplicação é superusuário ou ignora RLS.

## Tempos medidos

Medição local de 20/11/2026, com 29 lojas, 4.336 pedidos, 322 produtos e 1.028 registros de auditoria (35 MB):

| Etapa | Tempo |
| --- | --- |
| Backup | 1,6 s (dump de 3,5 MB) |
| Novo servidor pronto, com papéis | 3,4 s |
| Restauração | 1,5 s |
| Verificação | menos de 1 s |

Os tempos crescem com o volume de dados. Em produção, meça de novo a cada trimestre e sempre que o banco dobrar de tamanho.

## Objetivos

| Objetivo | Situação atual | Meta para produção (Fase 9) |
| --- | --- | --- |
| RPO, perda máxima de dados | Igual ao intervalo entre backups lógicos | 5 minutos, com arquivamento contínuo de WAL e recuperação a um ponto no tempo |
| RTO, tempo para voltar | Minutos para o volume atual, mais o tempo de apontar a API para o novo banco | 1 hora |

## Restauração real

1. Declare o incidente e pare a escrita: suspenda as lojas no console ou derrube a API e o worker.
2. Escolha o backup ou o ponto no tempo anterior ao problema.
3. Restaure em um servidor novo, nunca sobre o banco danificado, que serve como evidência.
4. Rode as verificações do ensaio contra o servidor restaurado.
5. Aponte `DB_HOST` da API, do worker e do serviço da plataforma para o novo servidor e suba os serviços.
6. Confira o smoke (`bash scripts/smoke.sh`) e os dashboards antes de liberar o tráfego.
7. Eventos no outbox são publicados pelo worker ao voltar. Pagamentos confirmados no provedor depois do ponto restaurado chegam de novo pelos webhooks do provedor.

## Cuidados

- O backup contém dados pessoais e tokens cifrados: guarde-o cifrado, com acesso restrito e auditado.
- Mensagens de e-mail pendentes estão cifradas com chave derivada de `MFA_ENCRYPTION_KEY`. Sem a mesma chave, o restaurado não consegue enviá-las.
