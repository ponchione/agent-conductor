export function formatName(first: string, last: string): string {
  return `${first} ${last}`;
}

export function validateEmail(email: string): boolean {
  return email.includes("@");
}
