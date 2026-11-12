import { browserSupportsWebAuthnAutofill, startAuthentication, startRegistration, WebAuthnAbortService } from "@simplewebauthn/browser";
import { api } from "../api";
export async function passkey(
  mode: "login" | "register",
  email?: string,
  password?: string,
  code?: string,
) {
  const root = mode === "login" ? "/auth/passkey" : "/security/passkey";
  const traceId = crypto.randomUUID().replaceAll("-", "");
  const begin = await api(root + "/begin", "POST", { email, password, code }, { traceId });
  const credential =
    mode === "login"
      ? await startAuthentication({ optionsJSON: begin.options.publicKey })
      : await startRegistration({ optionsJSON: begin.options.publicKey });
  await api(root + "/finish", "POST", credential, { traceId, passkeyToken: begin.token });
}

export async function conditionalPasskey(signal: AbortSignal): Promise<boolean> {
  if (!(await browserSupportsWebAuthnAutofill()) || signal.aborted) return false;
  const traceId = crypto.randomUUID().replaceAll("-", "");
  const begin = await api("/auth/passkey/begin", "POST", {}, { traceId });
  if (signal.aborted) return false;
  const cancel = () => WebAuthnAbortService.cancelCeremony();
  signal.addEventListener("abort", cancel, { once: true });
  try {
    const credential = await startAuthentication({ optionsJSON: begin.options.publicKey, useBrowserAutofill: true });
    if (signal.aborted) return false;
    await api("/auth/passkey/finish", "POST", credential, { traceId, passkeyToken: begin.token });
    return true;
  } catch (error) {
    if (signal.aborted || error instanceof Error && (error.name === "NotAllowedError" || error.name === "AbortError")) return false;
    throw error;
  } finally { signal.removeEventListener("abort", cancel); }
}

