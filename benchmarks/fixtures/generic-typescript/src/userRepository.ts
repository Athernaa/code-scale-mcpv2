export async function insert(record: any) {
  return { id: "generated", ...record };
}

export async function remove(id: string) {
  return id;
}
