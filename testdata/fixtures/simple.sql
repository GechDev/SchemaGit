CREATE TYPE public.user_role AS ENUM ('admin', 'member');
CREATE SEQUENCE public.user_id_seq;
CREATE TABLE public.users (
    id bigint NOT NULL DEFAULT nextval('public.user_id_seq'::regclass) CONSTRAINT users_id_pk PRIMARY KEY,
    role public.user_role NOT NULL,
    email text NOT NULL DEFAULT 'unknown',
    CONSTRAINT users_email_key UNIQUE (email)
);
CREATE INDEX users_email_idx ON public.users USING btree (email);
CREATE VIEW public.active_users AS SELECT id, email FROM public.users WHERE email <> '';