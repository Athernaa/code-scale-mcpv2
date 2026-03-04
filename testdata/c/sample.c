#define MAX_RETRIES 3

// User represents a user.
struct User {
    int id;
    char name[100];
};

// get_user returns a user by ID.
struct User get_user(int id) {
    struct User u;
    u.id = id;
    return u;
}

// authenticate checks a token.
int authenticate(const char* token) {
    return token != 0;
}
