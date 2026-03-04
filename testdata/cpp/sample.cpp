#include <string>

// UserService manages user operations.
class UserService {
public:
    // get_user returns a user name by ID.
    std::string get_user(int id) {
        return "user";
    }

    // delete_user deletes a user.
    bool delete_user(int id) {
        return true;
    }
};

// authenticate checks a token.
bool authenticate(const std::string& token) {
    return !token.empty();
}
