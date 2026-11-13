import { readState, writeState } from './greeting';

export function markGreetingSignedOut(userId: string) {
  const local = () => window.localStorage;
  const session = () => window.sessionStorage;
  const memory = readState(userId, local, session);
  // A storage fallback is deliberately limited to one greeting for the whole tab.
  if (!memory.available || memory.fallback) return;
  writeState(userId, { day: memory.state?.day ?? '0000-00-00', signedOut: true }, local, session);
}
