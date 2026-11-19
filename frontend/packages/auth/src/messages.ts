import { ApiError } from '@whitelabel/api-client'

const ERROR_MESSAGES: Record<string, string> = {
  INVALID_CREDENTIALS: 'E-mail ou senha incorretos.',
  INVALID_MFA_CODE: 'Código incorreto. Confira o aplicativo e tente novamente.',
  CHALLENGE_EXPIRED: 'A verificação expirou. Entre novamente.',
  INVALID_TOKEN: 'Este link é inválido ou já expirou. Solicite um novo.',
  INVALID_PASSWORD: 'A senha atual está incorreta.',
  RATE_LIMITED: 'Muitas tentativas. Aguarde alguns minutos e tente novamente.',
  SESSION_EXPIRED: 'Sua sessão expirou. Entre novamente.',
  PERMISSION_DENIED: 'Você não tem permissão para esta ação.',
  MEMBER_ALREADY_EXISTS: 'Este e-mail já faz parte da equipe.',
  LAST_ADMIN: 'A equipe precisa manter pelo menos um administrador ativo.',
  SELF_CHANGE_NOT_ALLOWED: 'Peça a outro administrador para fazer esta alteração.',
  MFA_REQUIRED: 'A verificação em duas etapas é obrigatória nesta conta.',
  NETWORK_ERROR: 'Não foi possível conectar ao servidor.',
}

const FIELD_MESSAGES: Record<string, string> = {
  'password:too_short': 'Use pelo menos 10 caracteres.',
  'password:too_long': 'Use no máximo 128 caracteres.',
  'password:common': 'Esta senha é muito comum. Escolha outra.',
  'password:contains_email': 'A senha não pode conter o seu e-mail.',
  'email:invalid': 'Informe um e-mail válido.',
  'name:invalid': 'Informe um nome com até 120 caracteres.',
  'role:invalid': 'Escolha um papel válido.',
}

/** Translates an API failure into a message for the person using the screen. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const field = error.fieldErrors[0]
    if (field) return FIELD_MESSAGES[`${field.field}:${field.code}`] ?? 'Confira os dados informados.'
    return ERROR_MESSAGES[error.code] ?? 'Não foi possível concluir a operação. Tente novamente.'
  }
  return 'Não foi possível concluir a operação. Tente novamente.'
}

const EVENT_LABELS: Record<string, string> = {
  login_succeeded: 'Acesso realizado',
  login_failed: 'Tentativa de acesso recusada',
}

export function eventLabel(event: string): string {
  return EVENT_LABELS[event] ?? event
}

export function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}
