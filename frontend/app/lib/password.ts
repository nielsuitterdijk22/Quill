// Password rules, mirroring Zitadel's complexity policy (the instance default:
// 8+ characters, upper, lower, number, symbol) and backend passwordProblem().
export type PasswordRule = { label: string; ok: boolean };

export function passwordRules(pw: string): PasswordRule[] {
  return [
    { label: "At least 8 characters", ok: pw.length >= 8 },
    { label: "An uppercase letter", ok: /\p{Lu}/u.test(pw) },
    { label: "A lowercase letter", ok: /\p{Ll}/u.test(pw) },
    { label: "A number", ok: /\p{Nd}/u.test(pw) },
    { label: "A symbol", ok: /[^\p{L}\p{Nd}]/u.test(pw) },
  ];
}

export const passwordOk = (pw: string) => pw.length <= 128 && passwordRules(pw).every((r) => r.ok);
