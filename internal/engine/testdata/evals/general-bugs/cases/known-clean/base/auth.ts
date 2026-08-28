const unauthorized = new Set(["INVALID"]);

export function classify(code: string): "unauthorized" | "retry" {
  return unauthorized.has(code) ? "unauthorized" : "retry";
}
