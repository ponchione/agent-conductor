from lib.auth import AuthService, validate_token

class AppService:
    def __init__(self):
        self.auth = AuthService()

    def process(self, token):
        if validate_token(token):
            return self.auth.authenticate(token)
        return False
