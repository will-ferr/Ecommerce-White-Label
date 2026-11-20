# Documentação da plataforma

Fase atual: **8, Observabilidade e segurança avançada**. A validação de um gateway real em sandbox segue pendente da escolha do provedor (veja o [roadmap](architecture/roadmap.md#pendências-registradas)). Para executar o projeto, veja [Primeiros passos](development/getting-started.md). O contrato da API está em [openapi.yaml](api/openapi.yaml).

## Entregas da Fase 1

| # | Entrega | Documento |
| --- | --- | --- |
| 1 | Visão geral da arquitetura | [system-overview.md](architecture/system-overview.md) |
| 2 | Diagramas C4 de contexto, containers, componentes e implantação | [system-overview.md](architecture/system-overview.md#2-c4-nível-1-contexto) |
| 3 | Organização dos módulos | [domain-model.md](architecture/domain-model.md) |
| 4 | Estratégia Multi-Tenant | [multi-tenancy.md](architecture/multi-tenancy.md) |
| 5 | Modelo inicial do banco de dados | [data-model.md](database/data-model.md) |
| 6 | Estratégia de segurança | [threat-model.md](security/threat-model.md) e [security-controls.md](security/security-controls.md) |
| 7 | Estratégia de observabilidade | [metrics.md](observability/metrics.md), [logging.md](observability/logging.md), [tracing.md](observability/tracing.md) e [alerts.md](observability/alerts.md) |
| 8 | Tecnologias e justificativas | [system-overview.md](architecture/system-overview.md#6-tecnologias-e-justificativas) e [ADRs](architecture/architecture-decisions.md) |
| 9 | Estrutura inicial de diretórios | [system-overview.md](architecture/system-overview.md#7-estrutura-inicial-de-diretórios) |
| 10 | Roadmap técnico | [roadmap.md](architecture/roadmap.md) |
| Extra | Riscos técnicos e arquiteturais | [risks.md](architecture/risks.md) |
| Extra | Arquitetura orientada a eventos | [event-driven-architecture.md](architecture/event-driven-architecture.md) |

