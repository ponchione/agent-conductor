from lib.service import AppService

def main():
    svc = AppService()
    svc.process("my-token")

if __name__ == "__main__":
    main()
