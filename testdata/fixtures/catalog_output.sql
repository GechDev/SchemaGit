-- Mirrors what a fresh PostgreSQL 15 catalog reports for testdata/fixtures/simple.sql:
-- deparsed expressions, implicit ::casts, no schema qualification in view bodies,
-- no USING clause on defaults and column order that differs from the DDL.
CREATE TYPE public.user_role AS ENUM ('admin', 'member');
CREATE SEQUENCE public.user_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
CREATE TABLE public.users (
    email text DEFAULT 'unknown'::text NOT NULL,
    id bigint DEFAULT nextval('public.user_id_seq'::regclass) NOT NULL,
    role public.user_role NOT NULL,
    CONSTRAINT users_id_pk PRIMARY KEY (id),
    CONSTRAINT users_email_key UNIQUE (email)
);
CREATE INDEX users_email_idx ON public.users USING btree (email);
CREATE VIEW public.active_users AS
    SELECT id, email FROM users WHERE (email <> ''::text);