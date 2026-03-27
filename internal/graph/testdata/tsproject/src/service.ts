import { formatName, validateEmail } from "./utils";

export class UserService {
  createUser(email: string, first: string, last: string): void {
    if (!validateEmail(email)) {
      throw new Error("Invalid email");
    }
    const name = formatName(first, last);
    console.log(`Created user: ${name}`);
  }
}
