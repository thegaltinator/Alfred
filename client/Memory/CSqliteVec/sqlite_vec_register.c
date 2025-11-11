#include "sqlite-vec.h"
#include <sqlite3.h>
#include <stddef.h>

int sqlite_vec_register(sqlite3 *db) {
    char *err = NULL;
    int rc = sqlite3_vec_init(db, &err, NULL);
    if (err) {
        sqlite3_free(err);
    }
    return rc;
}
