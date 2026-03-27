def validate_token(token):
    return len(token) > 0

class AuthService:
    def authenticate(self, token):
        if not validate_token(token):
            raise ValueError("Invalid token")
        return True

    def get_user(self, token):
        self.authenticate(token)
        return {"user": "test"}
