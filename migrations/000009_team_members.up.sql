CREATE TABLE IF NOT EXISTS team_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role VARCHAR(50) NOT NULL CHECK (role IN ('admin', 'accountant', 'viewer')),
    invite_email VARCHAR(255),
    invite_token VARCHAR(255),
    joined_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE
);

CREATE INDEX idx_team_members_business_id ON team_members(business_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_team_members_user_id ON team_members(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_team_members_invite_token ON team_members(invite_token) WHERE deleted_at IS NULL;
