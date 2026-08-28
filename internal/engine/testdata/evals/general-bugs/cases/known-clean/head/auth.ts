const unauthorized = new Set(["INVALID", "EXPIRED"]);

export function classify(code: string): "unauthorized" | "retry" {
  return unauthorized.has(code) ? "unauthorized" : "retry";
}
