\connect restaurants

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS vector;


CREATE TABLE IF NOT EXISTS restaurants
(
    id         SERIAL PRIMARY KEY,
    name       TEXT          NOT NULL,
    area       TEXT          NOT NULL,
    rating     NUMERIC(3, 1) NOT NULL,
    badges     TEXT[]        NULL,
    embedding  vector(768)   NULL,
    location   GEOGRAPHY(POINT, 4326) NULL,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);


CREATE TABLE IF NOT EXISTS menu_items
(
    id            SERIAL PRIMARY KEY,
    restaurant_id INTEGER REFERENCES restaurants ( id ) ON DELETE CASCADE,
    name          TEXT           NOT NULL,
    description   TEXT           NOT NULL,
    category      TEXT,
    price         NUMERIC(10, 2) NOT NULL,
    embedding     vector(768)    NULL,

    created_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- CREATE TABLE IF NOT EXISTS categories
-- (
--     id            SERIAL PRIMARY KEY,
--     name          TEXT        NOT NULL,
--     restaurant_id INTEGER REFERENCES restaurants ( id ) ON DELETE CASCADE,
--     embedding     vector(768) NULL,
--
--     created_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
--     updated_at    TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
-- );


CREATE INDEX IF NOT EXISTS restaurants_embedding_idx
    ON restaurants USING ivfflat ( embedding vector_cosine_ops )
    WITH (lists = 100);


CREATE INDEX IF NOT EXISTS menu_items_embedding_idx
    ON menu_items USING ivfflat ( embedding vector_cosine_ops )
    WITH (lists = 100);

-- CREATE INDEX IF NOT EXISTS categories_embedding_idx
--     ON categories USING ivfflat ( embedding vector_cosine_ops )
--     WITH (lists = 100);
