import { startAuthentication, startRegistration } from "@simplewebauthn/browser";
import { api } from "../api";
export async function passkey(
  mode: "login" | "register",
  email?: string,
  password?: string,
  code?: string,
) {
  const root = mode === "login" ? "/auth/passkey" : "/security/passkey";
  const begin = await api(root + "/begin", "POST", { email, password, code });
  const credential =
    mode === "login"
      ? await startAuthentication({ optionsJSON: begin.options.publicKey })
      : await startRegistration({ optionsJSON: begin.options.publicKey });
  const res = await fetch("/api/v1" + root + "/finish", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Passkey-Token": begin.token,
    },
    body: JSON.stringify(credential),
  });
  if (!res.ok) throw Error("Passkey 验证失败");
}

