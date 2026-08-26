export function validateUser(value: any) {
  if (!value || !value.email) throw new Error("email required");
  return value;
}

export function validateAdmin(value: any) {
  return Boolean(value && value.role === "admin");
}
