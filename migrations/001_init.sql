create table public.user_entity
(
    id                   uuid      default gen_random_uuid() not null
        primary key,
    firstname            varchar(255)                        not null,
    lastname             varchar(255)                        not null,
    email                varchar(255)                        not null,
    has_access           boolean   default false             not null,
    created_at           timestamp default now()             not null,
    ip_address           inet,
    access_granted_at    timestamp with time zone,
    access_granted_by    text
        constraint user_entity_access_granted_by_check
            check ((access_granted_by IS NULL) OR (access_granted_by = ANY (ARRAY ['scheduler'::text, 'admin'::text]))),
    access_revoked_at    timestamp with time zone,
    access_revoked_by    text,
    access_revoke_reason text,
    project_slug         text                                not null,
    constraint user_entity_revoke_pair_check
        check ((access_revoked_at IS NULL) = (access_revoke_reason IS NULL))
);

alter table public.user_entity
    owner to brain;

create index idx_user_entity_created_at
    on public.user_entity (created_at);

create unique index uq_user_entity_project_slug_email
    on public.user_entity (project_slug, email);

create index idx_user_entity_project_slug_access
    on public.user_entity (project_slug, has_access);

create table public.waiting_list
(
    id                  uuid      default gen_random_uuid() not null
        primary key,
    user_id             uuid                                not null
        unique
        references public.user_entity
            on delete cascade,
    created_at          timestamp default now()             not null,
    weight              integer   default 0                 not null,
    weighted_created_at timestamp generated always as ((created_at - ('01:00:00'::interval * (weight)::double precision))) stored,
    project_slug        text                                not null
)
    with (fillfactor = 70, autovacuum_vacuum_scale_factor = 0.05, autovacuum_vacuum_threshold = 50, autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 50);

alter table public.waiting_list
    owner to brain;

create index idx_waiting_list_weighted_created_at
    on public.waiting_list (weighted_created_at);

create index idx_waiting_list_user_id
    on public.waiting_list (user_id);

create index idx_waiting_list_project_slug_weighted
    on public.waiting_list (project_slug, weighted_created_at);

create table public.scheduler_state
(
    key          varchar(100)            not null,
    value        timestamp default now() not null,
    project_slug text                    not null,
    primary key (project_slug, key)
);

alter table public.scheduler_state
    owner to brain;

