import { insert } from "./userRepository";

export async function saveUser(input: any) {
  const normalized = normalizeUser(input);
  return insert(normalized);
}

export function normalizeUser(input: any) {
  return { ...input, email: String(input.email).trim().toLowerCase() };
}
