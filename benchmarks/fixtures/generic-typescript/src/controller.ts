import { saveUser } from "./userService";
import { validateUser } from "./validators";

export async function createUser(request: any, response: any) {
  const input = request.body;
  validateUser(input);
  const saved = await saveUser(input);
  response.json(saved);
  return saved;
}

export function health() {
  return { ok: true };
}
